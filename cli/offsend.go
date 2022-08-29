package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var offlineSendCmd = &cli.Command{
	Name:      "offline-send",
	Usage:     "Offline send funds between accounts",
	ArgsUsage: "[targetAddress] [amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "key",
			Usage: "from address  key",
		},
		&cli.Float64Flag{
			Name:  "gas-feecap",
			Usage: "gas feecap for new message",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return ShowHelp(cctx, fmt.Errorf("'send' expects two arguments, target and amount"))
		}

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		ownerKey := types.KeyInfo{}
		key1, err := hex.DecodeString(cctx.String("key"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(key1, &ownerKey); err != nil {
			return err
		}

		toAddr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to parse target address: %w", err))
		}

		val, err := types.ParseFIL(cctx.Args().Get(1))
		if err != nil {
			return ShowHelp(cctx, fmt.Errorf("failed to parse amount: %w", err))
		}

		fromAddr, err := address.NewFromString(cctx.String("from"))
		if err != nil {
			return err
		}
		nonce, err := api.MpoolGetNonce(ctx, fromAddr)
		if err != nil {
			return err
		}
		fmt.Println("nonce: ", nonce)

		msg := &types.Message{
			From:   fromAddr,
			To:     toAddr,
			Value:  types.BigInt(val),
			Method: builtin.MethodSend,
			Nonce:  nonce,
		}

		msg, err = api.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			return err
		}
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
		fmt.Printf("send message %s\n", ccid.String())
		return nil
	},
}
