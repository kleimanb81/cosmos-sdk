package valuerenderer

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type ValueRenderer interface {
	Format(interface{}) (string, error)
	Parse(string) (interface{}, error)
}

// create default value rreenderer in CLI and then get context from CLI
type DefaultValueRenderer struct {
	// /string is denom that user sents
	displayDenomToMetadataMap map[string]banktypes.Metadata // define in test only //convert DenomUnits to Display units
}

// TODO consider to move into valuerenderer_test.go
// TODO handle an entire slice
func NewDefaultValueRenderer() DefaultValueRenderer {
	return DefaultValueRenderer{displayDenomToMetadataMap: make(map[string]banktypes.Metadata)}
}

func (dvr DefaultValueRenderer) SetDenomToMetadataMap(metadatas []banktypes.Metadata) error {
	// TODO should I validate denom?
	if metadatas == nil {
		return errors.New("empty metadatas")
	}

	for _, m := range metadatas {
		dvr.displayDenomToMetadataMap[m.Display] = m
	}

	return nil
}

var _ ValueRenderer = &DefaultValueRenderer{}

// Format converts an empty interface into a string depending on interface type.
func (dvr DefaultValueRenderer) Format(x interface{}) (string, error) {
	p := message.NewPrinter(language.English)
	var sb strings.Builder

	switch x.(type) {
	case types.Dec:
		i, ok := x.(types.Dec)
		if !ok {
			return "", errors.New("unable to cast interface{} to Int")
		}

		s := i.String()
		if len(s) == 0 {
			return "", errors.New("empty string")
		}

		strs := strings.Split(s, ".")

		// TODO should I address cases with len(strs) > 2 and others
		if len(strs) == 2 {
			// there is a decimal place

			// format the first part
			i64, err := strconv.ParseInt(strs[0], 10, 64)
			if err != nil {
				return "", errors.New("unable to convert string to int64")
			}
			formated := p.Sprintf("%d", i64)

			// concatanate first part, "." and second part
			sb.WriteString(formated)
			sb.WriteString(".")
			sb.WriteString(strs[1])
		}

	case types.Int:
		i, ok := x.(types.Int)
		if !ok {
			return "", errors.New("unable to cast interface{} to Int")
		}

		s := i.String()
		if len(s) == 0 {
			return "", errors.New("empty string")
		}

		sb.WriteString(p.Sprintf("%d", i.Int64()))

	case types.Coin:
		coin, ok := x.(types.Coin)
		if !ok {
			return "", errors.New("unable to cast empty interface to Coin")
		}

		metadata, err := dvr.LookupMetadataByDenom(ConvertDenomToDisplay(coin.Denom))
		if err != nil {
			return "", err
		}

		newAmount, newDenom := p.Sprintf("%d", dvr.ComputeAmount(coin, metadata)), metadata.Display
		sb.WriteString(newAmount)
		sb.WriteString(newDenom)

		//	default:
		//		panic("type is invalid")
	}

	return sb.String(), nil
}

func (dvr DefaultValueRenderer) LookupMetadataByDenom(denom string) (banktypes.Metadata, error) {
	// lookup metadata by displayDenom
	metadata, ok := dvr.displayDenomToMetadataMap[denom]
	if !ok {
		return banktypes.Metadata{}, errors.New("unable to lookup displayDenom in displayDenomToMetadataMap")
	}

	return metadata,nil
}


func (dvr DefaultValueRenderer) ComputeAmount(coin types.Coin, metadata banktypes.Metadata) int64 {

	var coinExp, displayExp int64
	for _, denomUnit := range metadata.DenomUnits {
		if denomUnit.Denom == coin.Denom {
			coinExp = int64(denomUnit.Exponent)
		}

		if denomUnit.Denom == metadata.Display {
			displayExp = int64(denomUnit.Exponent)
		}
	}

	expSub := float64(displayExp - coinExp)
	var amount int64

	switch {
	// negative , convert mregen to regen less zeroes
	case math.Signbit(expSub):
		// TODO or should i use math package?
		amount = types.NewDecFromIntWithPrec(coin.Amount, int64(math.Abs(expSub))).TruncateInt64() // use Dec or just golang built in methods
	// positive, convert mregen to uregen
	case !math.Signbit(expSub):
		amount = coin.Amount.Mul(types.NewInt(int64(math.Pow(10, expSub)))).Int64()
	// == 0, convert regen to regen, amount does not change
	default:
		amount = coin.Amount.Int64()
	}

	return amount
}

// mregen => regen, uregen => regen
func ConvertDenomToDisplay(denom string) string {
	if strings.HasPrefix(denom, "u") || strings.HasPrefix(denom, "m") {
		denom = denom[1:]
	}

	return denom
}

// see QueryDenomMetadataRequest() test
/*
func (dvr DefaultValueRenderer) denomQuerier() banktypes.Metadata {


		app := simapp.Setup(t, false)
		ctx := app.BaseApp.NewContext(false, tmproto.Header{})

		queryHelper := baseapp.NewQueryServerTestHelper(ctx, app.InterfaceRegistry())
		types.RegisterQueryServer(queryHelper, app.BankKeeper)
		queryClient := types.NewQueryClient(queryHelper)

		req := &types.QueryDenomsMetadataRequest{
			Pagination: &query.PageRequest{
				Limit:      3,
				CountTotal: true,
			},
		}

		res, err := queryClient.DenomsMetadata(ctx, req)

	// TODO make argument in denomQuerier to set Metadata.Display to convert between mregen and uregen
	return banktypes.Metadata{
		Description: "The native staking token of the Cosmos Hub.",
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    "regen",
				Exponent: 0,
				Aliases:  []string{"regen"},
			},
			{
				Denom:    "uregen",
				Exponent: 6,
				Aliases:  []string{"microregen"},
			},
			{
				Denom:    "mregen",
				Exponent: 3,
				Aliases:  []string{"miniregen"},
			},
		},
		Base:    "uregen",
		Display: "regen",
	}
}
*/

// Parse parses string and takes a decision whether to convert it into Coin or Uint
func (dvr DefaultValueRenderer) Parse(s string) (interface{}, error) {
	if s == "" {
		return nil, errors.New("unable to parse empty string")
	}

	str := strings.ReplaceAll(s, ",", "")
	re := regexp.MustCompile(`\d+[mu]?regen`)
	// case 1: "1000000regen" => Coin
	if re.MatchString(str) {
		coin, err := coinFromString(str)
		if err != nil {
			return nil, err
		}

		return coin, nil
	}

	// case2: convert it to Uint
	i, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return nil, err
	}

	return types.NewUint(i), nil
}

func coinFromString(s string) (types.Coin, error) {
	index := len(s) - 1
	for i := len(s) - 1; i >= 0; i-- {
		if unicode.IsLetter(rune(s[i])) {
			continue
		}

		index = i
		break
	}

	if index == len(s)-1 {
		return types.Coin{}, errors.New("no denom has been found")
	}

	denom := s[index+1:]
	amount := s[:index+1]
	// convert to int64 to make up Coin later
	amountInt, ok := types.NewIntFromString(amount)
	if !ok {
		return types.Coin{}, errors.New("unable convert amountStr to int64")
	}

	return types.NewCoin(denom, amountInt), nil
}