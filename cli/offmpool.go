package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
	"strconv"
)

var offlineMpoolReplaceCmd = &cli.Command{
	Name:  "offline-replace",
	Usage: "Offline replace a message in the mempool",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "key",
			Usage: "owner key",
		},
		&cli.StringFlag{
			Name:  "gas-feecap",
			Usage: "gas feecap for new message",
		},
		&cli.StringFlag{
			Name:  "gas-premium",
			Usage: "gas price for new message",
		},
		&cli.Int64Flag{
			Name:  "gas-limit",
			Usage: "gas price for new message",
			Value: 0,
		},
		&cli.BoolFlag{
			Name:  "auto",
			Usage: "automatically reprice the specified message",
		},
		&cli.StringFlag{
			Name:  "max-fee",
			Usage: "Spend up to X FIL for this message (applicable for auto mode)",
		},
	},
	ArgsUsage: "<from nonce> | <message-cid>",
	Action: func(cctx *cli.Context) error {

		api, closer, err := GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)

		var from address.Address
		var nonce uint64
		switch cctx.Args().Len() {
		case 1:
			mcid, err := cid.Decode(cctx.Args().First())
			if err != nil {
				return err
			}

			msg, err := api.ChainGetMessage(ctx, mcid)
			if err != nil {
				return fmt.Errorf("could not find referenced message: %w", err)
			}

			from = msg.From
			nonce = msg.Nonce
		case 2:
			f, err := address.NewFromString(cctx.Args().Get(0))
			if err != nil {
				return err
			}

			n, err := strconv.ParseUint(cctx.Args().Get(1), 10, 64)
			if err != nil {
				return err
			}

			from = f
			nonce = n
		default:
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		ts, err := api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting chain head: %w", err)
		}

		pending, err := api.MpoolPending(ctx, ts.Key())
		if err != nil {
			return err
		}

		var found *types.SignedMessage
		for _, p := range pending {
			if p.Message.From == from && p.Message.Nonce == nonce {
				found = p
				break
			}
		}

		if found == nil {
			return fmt.Errorf("no pending message found from %s with nonce %d", from, nonce)
		}

		msg := found.Message

		if cctx.Bool("auto") {
			minRBF := messagepool.ComputeMinRBF(msg.GasPremium)

			var mss *lapi.MessageSendSpec
			if cctx.IsSet("max-fee") {
				maxFee, err := types.BigFromString(cctx.String("max-fee"))
				if err != nil {
					return fmt.Errorf("parsing max-spend: %w", err)
				}
				mss = &lapi.MessageSendSpec{
					MaxFee: maxFee,
				}
			}

			// msg.GasLimit = 0 // TODO: need to fix the way we estimate gas limits to account for the messages already being in the mempool
			msg.GasFeeCap = abi.NewTokenAmount(0)
			msg.GasPremium = abi.NewTokenAmount(0)
			retm, err := api.GasEstimateMessageGas(ctx, &msg, mss, types.EmptyTSK)
			if err != nil {
				return fmt.Errorf("failed to estimate gas values: %w", err)
			}

			msg.GasPremium = big.Max(retm.GasPremium, minRBF)
			msg.GasFeeCap = big.Max(retm.GasFeeCap, msg.GasPremium)
			gaslimit := cctx.Int64("gas-limit")
			if gaslimit > 0 {
				msg.GasLimit = gaslimit
			}

			gasFeeCap := cctx.Float64("gas-feecap")
			if gasFeeCap > 0 {
				fmt.Println("gasFeeCap ", gasFeeCap)
				if gasFeeCap < 20 {
					gasFeeCap = gasFeeCap * FilNATO * 10
				} else if gasFeeCap >= 10*FilNATO && gasFeeCap <= 90*FilNATO {
					fmt.Println("change gasfeeCap to less than 20 ", gasFeeCap)
				} else {
					return fmt.Errorf("failed to estimate gas values")
				}
				msg.GasFeeCap = abi.NewTokenAmount(int64(gasFeeCap))
				fmt.Println("gasFeeCap fixed ", gasFeeCap)
			}

			mff := func() (abi.TokenAmount, error) {
				return abi.TokenAmount(config.DefaultDefaultMaxFee), nil
			}
			messagepool.CapGasFee(mff, &msg, mss)
			fmt.Println("gasFeeCap fixed Nonce:", msg.Nonce, " GasFeeCap:", msg.GasFeeCap)
		} else {
			if cctx.IsSet("gas-limit") {
				msg.GasLimit = cctx.Int64("gas-limit")
			}
			msg.GasPremium, err = types.BigFromString(cctx.String("gas-premium"))
			if err != nil {
				return fmt.Errorf("parsing gas-premium: %w", err)
			}
			// TODO: estimate fee cap here
			msg.GasFeeCap, err = types.BigFromString(cctx.String("gas-feecap"))
			if err != nil {
				return fmt.Errorf("parsing gas-feecap: %w", err)
			}
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

		sig, err := sigs.Sign(key.ActSigType(ownerKey.Type), ownerKey.PrivateKey, mb.Cid().Bytes())
		if err != nil {
			return err
		}

		msgsigned := &types.SignedMessage{
			Message:   msg,
			Signature: *sig,
		}

		cid, err := api.MpoolPush(ctx, msgsigned)
		if err != nil {
			return fmt.Errorf("failed to push new message to mempool: %w", err)
		}

		fmt.Println("new message cid: ", cid)
		return nil
	},
}
