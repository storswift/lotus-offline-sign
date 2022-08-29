package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"

	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
)

var offlineWithdrawCmd = &cli.Command{
	Name:      "offline-withdraw",
	Usage:     "Offline withdraw available balance",
	ArgsUsage: "[amount (FIL)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "key",
			Usage: "owner key",
		},
		&cli.StringFlag{
			Name:  "maddr",
			Usage: "minerId",
		},
		&cli.StringFlag{
			Name:  "owner",
			Usage: "owner address",
		},
		&cli.Int64Flag{
			Name:  "premium",
			Usage: "gas premium",
		},
		&cli.Float64Flag{
			Name:  "gas-feecap",
			Usage: "gas feecap for new message",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, acloser, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()
		ctx := ReqContext(cctx)

		ver, err := api.Version(ctx)
		if err != nil {
			return err
		}
		fmt.Println("lotus version: ", ver.String())

		ownerKey := types.KeyInfo{}

		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}

		maddr, err := address.NewFromString(cctx.String("maddr"))
		if err != nil {
			return err
		}
		fmt.Println("maddr: ", maddr.String())

		available, err := api.StateMinerAvailableBalance(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		amount := available
		if cctx.Args().Present() {
			f, err := types.ParseFIL(cctx.Args().First())
			if err != nil {
				return xerrors.Errorf("parsing 'amount' argument: %w", err)
			}

			amount = abi.TokenAmount(f)

			if amount.GreaterThan(available) {
				return xerrors.Errorf("can't withdraw more funds than available; requested: %s; available: %s", amount, available)
			}
		}

		params, err := actors.SerializeParams(&miner2.WithdrawBalanceParams{
			AmountRequested: amount, // Default to attempting to withdraw all the extra funds in the miner actor
		})
		if err != nil {
			return err
		}

		ownerAddr, err := address.NewFromString(cctx.String("owner"))
		if err != nil {
			return err
		}
		fmt.Println("ownerAddr: ", ownerAddr.String())
		nonce, err := api.MpoolGetNonce(ctx, ownerAddr)
		if err != nil {
			return err
		}
		fmt.Println("nonce: ", nonce)
		msg := &types.Message{
			To:     maddr,
			From:   ownerAddr,
			Value:  types.NewInt(0),
			Method: builtin.MethodsMiner.WithdrawBalance,
			Params: params,
			Nonce:  nonce,
			//GasPremium: abi.NewTokenAmount(cctx.Int64("premium")),
			//GasFeeCap:  abi.NewTokenAmount(cctx.Int64("feeCap")),
		}

		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}
		//msg.GasLimit = gasLimit
		if cctx.Float64("gas-feecap") != 0 {
			gasFeeCap := cctx.Float64("gas-feecap")
			gasFeeCap = gasFeeCap * FilNATO * 10
			msg.GasFeeCap = abi.NewTokenAmount(int64(gasFeeCap))
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
		fmt.Printf("Requested rewards withdrawal in message %s\n", ccid.String())
		return nil
	},
}
