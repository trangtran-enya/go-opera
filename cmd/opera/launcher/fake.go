package launcher

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/Fantom-foundation/lachesis-base/inter/idx"
	"github.com/ethereum/go-ethereum/common"
	cli "gopkg.in/urfave/cli.v1"

	"github.com/Fantom-foundation/go-opera/integration/makefakegenesis"
)

// FakeNetFlag enables special testnet, where validators are automatically created
var FakeNetFlag = cli.StringFlag{
	Name:  "fakenet",
	Usage: "'n/N[,non-validators]' - sets coinbase as fake n-th key from genesis of N validators. Non-validators is json-file.",
}

func getFakeValidatorKey(ctx *cli.Context) *ecdsa.PrivateKey {
	id, _, _, err := parseFakeGen(ctx.GlobalString(FakeNetFlag.Name))
	if err != nil || id == 0 {
		return nil
	}
	return makefakegenesis.FakeKey(id)
}

func parseFakeGen(s string) (id idx.ValidatorID, num idx.Validator, accs map[common.Address]*big.Int, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("use %%d/%%d format")
		return
	}

	var u32 uint64
	u32, err = strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return
	}
	id = idx.ValidatorID(u32)

	parts = strings.SplitN(parts[1], ",", 2)

	u32, err = strconv.ParseUint(parts[0], 10, 32)
	num = idx.Validator(u32)
	if num < 0 || idx.Validator(id) > num {
		err = fmt.Errorf("key-num should be in range from 1 to validators (<key-num>/<validators>), or should be zero for non-validator node")
		return
	}

	if len(parts) < 2 {
		return
	}

	accs, err = readAccounts(parts[1])

	return
}

type Account struct {
	Balance    *big.Int          `json:"balance"`
	PrivateKey *ecdsa.PrivateKey `json:"PrivateKey"`
}
type Accounts map[common.Address]Account

func readAccounts(filename string) (accs map[common.Address]*big.Int, err error) {
	var f *os.File
	f, err = os.Open(filename)
	if err != nil {
		return
	}

	rawAccs := Accounts{}
	err = json.NewDecoder(f).Decode(&rawAccs)
	if err != nil {
		return
	}

	accs = make(map[common.Address]*big.Int, len(rawAccs))
	for address, account := range rawAccs {
		accs[address] = account.Balance
	}

	return
}
