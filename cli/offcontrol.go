package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	wallet "github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var offlineActorControl = &cli.Command{
	Name:  "offline-control",
	Usage: "Offline manage control addresses",
	Subcommands: []*cli.Command{
		actorControlSet,
		actorProposeChangeWorker,
		actorConfirmChangeWorker,
	},
}

var actorControlSet = &cli.Command{
	Name:      "set",
	Usage:     "Set control address(-es)",
	ArgsUsage: "[...address]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "maddr",
			Usage: "miner address",
		},
		&cli.StringFlag{
			Name:  "owner",
			Usage: "owner address",
		},
		&cli.StringFlag{
			Name:  "key",
			Usage: "owner key",
		},
	},
	Action: func(cctx *cli.Context) error {

		api, acloser, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.String("maddr"))
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		del := map[address.Address]struct{}{}
		existing := map[address.Address]struct{}{}
		for _, controlAddress := range mi.ControlAddresses {
			ka, err := api.StateAccountKey(ctx, controlAddress, types.EmptyTSK)
			if err != nil {
				return err
			}

			del[ka] = struct{}{}
			existing[ka] = struct{}{}
		}

		var toSet []address.Address

		for i, as := range cctx.Args().Slice() {
			a, err := address.NewFromString(as)
			if err != nil {
				return xerrors.Errorf("parsing address %d: %w", i, err)
			}

			ka, err := api.StateAccountKey(ctx, a, types.EmptyTSK)
			if err != nil {
				return err
			}

			// make sure the address exists on chain
			_, err = api.StateLookupID(ctx, ka, types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("looking up %s: %w", ka, err)
			}

			delete(del, ka)
			toSet = append(toSet, ka)
		}

		for a := range del {
			fmt.Println("Remove", a)
		}
		for _, a := range toSet {
			if _, exists := existing[a]; !exists {
				fmt.Println("Add", a)
			}
		}

		cwp := &miner2.ChangeWorkerAddressParams{
			NewWorker:       mi.Worker,
			NewControlAddrs: toSet,
		}

		sp, err := actors.SerializeParams(cwp)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		owner, err := address.NewFromString(cctx.String("owner"))
		if err != nil {
			return err
		}

		msg := &types.Message{
			From:   owner,
			To:     maddr,
			Method: builtin.MethodsMiner.ChangeWorkerAddress,
			Value:  big.Zero(),
			Params: sp,
		}
		nonce, err := api.MpoolGetNonce(context.Background(), owner)
		if err != nil {
			return err
		}
		msg.Nonce = nonce
		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}

		ownerKey := types.KeyInfo{}

		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}
		mb, err := msg.ToStorageBlock()
		if err != nil {
			return xerrors.Errorf("serializing message: %w", err)
		}

		sig, err := sigs.Sign(wallet.ActSigType(ownerKey.Type), ownerKey.PrivateKey, mb.Cid().Bytes())
		if err != nil {
			return err
		}

		msgsigned := types.SignedMessage{
			Message:   *msg,
			Signature: *sig,
		}

		ccid, err := api.MpoolPush(ctx, &msgsigned)
		if err != nil {
			return xerrors.Errorf("mpool push: %w", err)
		}

		fmt.Println("Message CID:", ccid.String())

		return nil
	},
}

var actorProposeChangeWorker = &cli.Command{
	Name:  "propose-change-worker",
	Usage: "Propose a worker address change",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "maddr",
			Usage: "miner address",
		},
		&cli.StringFlag{
			Name:  "owner",
			Usage: "owner full  address",
		},
		&cli.StringFlag{
			Name:  "key",
			Usage: "owner key",
		},
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually send transaction performing the action",
			Value: false,
		},
		&cli.StringFlag{
			Name:  "newaddr",
			Usage: "new worker address",
		},
	},
	Action: func(cctx *cli.Context) error {

		api, acloser, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := ReqContext(cctx)

		owner, err := address.NewFromString(cctx.String("owner"))
		if err != nil {
			return err
		}

		maddr, err := address.NewFromString(cctx.String("maddr"))
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		newmaddr, err := address.NewFromString(cctx.String("newaddr"))
		if err != nil {
			return err
		}

		newAddr, err := api.StateLookupID(ctx, newmaddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		if mi.NewWorker.Empty() {
			if mi.Worker == newAddr {
				return fmt.Errorf("worker address already set to %s", maddr)
			}
		} else {
			if mi.NewWorker == newAddr {
				return fmt.Errorf("change to worker address %s already pending", maddr)
			}
		}

		if !cctx.Bool("really-do-it") {
			fmt.Fprintln(cctx.App.Writer, "Pass --really-do-it to actually execute this action")
			return nil
		}

		fmt.Fprintln(cctx.App.Writer, " addr ", owner, maddr, newAddr)

		if mi.Owner != owner {
			fmt.Fprintln(cctx.App.Writer, "mi.Owner != owner ", mi.Owner, owner)
			//return nil
		}

		cwp := &miner2.ChangeWorkerAddressParams{
			NewWorker:       newAddr,
			NewControlAddrs: mi.ControlAddresses,
		}

		sp, err := actors.SerializeParams(cwp)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		msg := &types.Message{
			From:   owner,
			To:     maddr,
			Method: builtin.MethodsMiner.ChangeWorkerAddress,
			Value:  big.Zero(),
			Params: sp,
		}

		nonce, err := api.MpoolGetNonce(context.Background(), owner)
		if err != nil {
			return err
		}
		msg.Nonce = nonce
		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}

		ownerKey := types.KeyInfo{}

		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}
		mb, err := msg.ToStorageBlock()
		if err != nil {
			return xerrors.Errorf("serializing message: %w", err)
		}

		sig, err := sigs.Sign(wallet.ActSigType(ownerKey.Type), ownerKey.PrivateKey, mb.Cid().Bytes())
		if err != nil {
			return err
		}

		msgsigned := types.SignedMessage{
			Message:   *msg,
			Signature: *sig,
		}

		smsg, err := api.MpoolPush(ctx, &msgsigned)
		if err != nil {
			return xerrors.Errorf("mpool push: %w", err)
		}

		fmt.Fprintln(cctx.App.Writer, "Propose Message CID:", smsg)

		// wait for it to get mined into a block
		wait, err := api.StateWaitMsg(ctx, smsg, build.MessageConfidence)
		if err != nil {
			return err
		}

		// check it executed successfully
		if wait.Receipt.ExitCode != 0 {
			fmt.Fprintln(cctx.App.Writer, "Propose worker change failed!")
			return err
		}

		mi, err = api.StateMinerInfo(ctx, maddr, wait.TipSet)
		if err != nil {
			return err
		}
		if mi.NewWorker != newAddr {
			return fmt.Errorf("Proposed worker address change not reflected on chain: expected '%s', found '%s'", maddr, mi.NewWorker)
		}

		fmt.Fprintf(cctx.App.Writer, "Worker key change to %s successfully proposed.\n", maddr)
		fmt.Fprintf(cctx.App.Writer, "Call 'confirm-change-worker' at or after height %d to complete.\n", mi.WorkerChangeEpoch)

		return nil
	},
}

var actorConfirmChangeWorker = &cli.Command{
	Name:  "confirm-change-worker",
	Usage: "Confirm a worker address change",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "maddr",
			Usage: "miner address",
		},
		&cli.StringFlag{
			Name:  "owner",
			Usage: "owner full address",
		},
		&cli.StringFlag{
			Name:  "key",
			Usage: "owner key",
		},
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually send transaction performing the action",
			Value: false,
		},
		&cli.StringFlag{
			Name:  "newaddr",
			Usage: "new worker address",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, acloser, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx := ReqContext(cctx)

		maddr, err := address.NewFromString(cctx.String("maddr"))
		if err != nil {
			return err
		}

		owner, err := address.NewFromString(cctx.String("owner"))
		if err != nil {
			return err
		}

		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		mnewaddr, err := address.NewFromString(cctx.String("newaddr"))
		if err != nil {
			return err
		}

		newAddr, err := api.StateLookupID(ctx, mnewaddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		if mi.NewWorker.Empty() {
			return xerrors.Errorf("no worker key change proposed")
		} else if mi.NewWorker != newAddr {
			return xerrors.Errorf("worker key %s does not match current worker key proposal %s", newAddr, mi.NewWorker)
		}

		if head, err := api.ChainHead(ctx); err != nil {
			return xerrors.Errorf("failed to get the chain head: %w", err)
		} else if head.Height() < mi.WorkerChangeEpoch {
			return xerrors.Errorf("worker key change cannot be confirmed until %d, current height is %d", mi.WorkerChangeEpoch, head.Height())
		}

		if !cctx.Bool("really-do-it") {
			fmt.Fprintln(cctx.App.Writer, "Pass --really-do-it to actually execute this action")
			return nil
		}

		if mi.Owner != owner {
			fmt.Fprintln(cctx.App.Writer, "mi.Owner != owner ", mi.Owner, owner)
			return nil
		}

		msg := &types.Message{
			From:   owner,
			To:     maddr,
			Method: builtin.MethodsMiner.ConfirmChangeWorkerAddress,
			Value:  big.Zero(),
		}

		nonce, err := api.MpoolGetNonce(context.Background(), owner)
		if err != nil {
			return err
		}
		msg.Nonce = nonce
		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}

		ownerKey := types.KeyInfo{}

		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}
		mb, err := msg.ToStorageBlock()
		if err != nil {
			return xerrors.Errorf("serializing message: %w", err)
		}

		sig, err := sigs.Sign(wallet.ActSigType(ownerKey.Type), ownerKey.PrivateKey, mb.Cid().Bytes())
		if err != nil {
			return err
		}

		msgsigned := types.SignedMessage{
			Message:   *msg,
			Signature: *sig,
		}

		smsg, err := api.MpoolPush(ctx, &msgsigned)
		if err != nil {
			return xerrors.Errorf("mpool push: %w", err)
		}

		fmt.Fprintln(cctx.App.Writer, "Confirmed Message CID:", smsg)

		// wait for it to get mined into a block
		wait, err := api.StateWaitMsg(ctx, smsg, build.MessageConfidence)
		if err != nil {
			return err
		}

		// check it executed successfully
		if wait.Receipt.ExitCode != 0 {
			fmt.Fprintln(cctx.App.Writer, "Worker change failed!")
			return err
		}

		mi, err = api.StateMinerInfo(ctx, maddr, wait.TipSet)
		if err != nil {
			return err
		}
		if mi.Worker != newAddr {
			return fmt.Errorf("Confirmed worker address change not reflected on chain: expected '%s', found '%s'", newAddr, mi.Worker)
		}

		return nil
	},
}

