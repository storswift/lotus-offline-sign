package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var offlineActorSetOwnerCmd = &cli.Command{
	Name:      "offline-set-owner",
	Usage:     "Offline set owner address",
	ArgsUsage: "[address]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "actor",
			Value:   "",
			Usage:   "specify other actor to check state for (read only)",
			Aliases: []string{"a"},
		},
	},
	Subcommands: []*cli.Command{
		oneSetOwnerCmd,
		twoSetOwnerCmd,
	},
}

var oneSetOwnerCmd = &cli.Command{
	Name:      "one",
	Usage:     "Set owner address",
	ArgsUsage: "[address]",
	Flags: []cli.Flag{
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

		ownerKey := types.KeyInfo{}

		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}

		na, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		newAddr, err := api.StateLookupID(ctx, na, types.EmptyTSK)
		if err != nil {
			return err
		}

		maddr, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return err
		}

		sp, err := actors.SerializeParams(&newAddr)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		ownerAddr, err := address.NewFromString(cctx.String("owner"))
		if err != nil {
			return err
		}
		nonce, err := api.MpoolGetNonce(ctx, ownerAddr)
		if err != nil {
			return err
		}
		msg := &types.Message{
			From:   ownerAddr,
			To:     maddr,
			Method: builtin.MethodsMiner.ChangeOwnerAddress,
			Value:  big.Zero(),
			Params: sp,
			Nonce:  nonce,
		}

		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}

		mb, err := msg.ToStorageBlock()
		if err != nil {
			return xerrors.Errorf("serializing message: %w", err)
		}

		sig, err := sigs.Sign(key.ActSigType(ownerKey.Type), ownerKey.PrivateKey, mb.Cid().Bytes())
		if err != nil {
			return err
		}

		msgsigned := types.SignedMessage{
			Message:   *msg,
			Signature: *sig,
		}

		ccid, err := api.MpoolPush(ctx, &msgsigned)
		if err != nil {
			return err
		}
		fmt.Printf("oneSetOwnerCmd in message %s\n", ccid.String())
		return nil
	},
}

var twoSetOwnerCmd = &cli.Command{
	Name:      "two",
	Usage:     "Set owner address",
	ArgsUsage: "[address]",
	Flags: []cli.Flag{

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

		ownerKey := types.KeyInfo{}

		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}

		maddr, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return err
		}

		ownerAddr, err := address.NewFromString(cctx.String("owner"))
		if err != nil {
			return err
		}

		nonce, err := api.MpoolGetNonce(ctx, ownerAddr)
		if err != nil {
			return err
		}

		newAddr, err := api.StateLookupID(ctx, ownerAddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		sp, err := actors.SerializeParams(&newAddr)
		if err != nil {
			return xerrors.Errorf("serializing params: %w", err)
		}

		msg := &types.Message{
			From:   ownerAddr,
			To:     maddr,
			Method: builtin.MethodsMiner.ChangeOwnerAddress,
			Value:  big.Zero(),
			Params: sp,
			Nonce:  nonce,
		}

		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}

		mb, err := msg.ToStorageBlock()
		if err != nil {
			return xerrors.Errorf("serializing message: %w", err)
		}

		sig, err := sigs.Sign(key.ActSigType(ownerKey.Type), ownerKey.PrivateKey, mb.Cid().Bytes())
		if err != nil {
			return err
		}

		msgsigned := types.SignedMessage{
			Message:   *msg,
			Signature: *sig,
		}

		ccid, err := api.MpoolPush(ctx, &msgsigned)
		if err != nil {
			return err
		}
		fmt.Printf("twoSetOwnerCmd in message %s\n", ccid.String())
		return nil
	},
}
