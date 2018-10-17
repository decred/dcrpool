package worker

import (
	"math/big"
	"testing"
)

func TestCalculateDividend(t *testing.T) {
	set := map[string]struct {
		input  []*Share
		output map[string]*big.Rat
		err    error
	}{
		"equal shares": {
			input: []*Share{
				NewShare("a", new(big.Rat).SetInt64(5)),
				NewShare("b", new(big.Rat).SetInt64(5)),
				NewShare("c", new(big.Rat).SetInt64(5)),
				NewShare("d", new(big.Rat).SetInt64(5)),
				NewShare("e", new(big.Rat).SetInt64(5)),
			},
			output: map[string]*big.Rat{
				"a": new(big.Rat).SetFrac64(5, 25),
				"b": new(big.Rat).SetFrac64(5, 25),
				"c": new(big.Rat).SetFrac64(5, 25),
				"d": new(big.Rat).SetFrac64(5, 25),
				"e": new(big.Rat).SetFrac64(5, 25),
			},
			err: nil,
		},
		"inequal shares": {
			input: []*Share{
				NewShare("a", new(big.Rat).SetInt64(5)),
				NewShare("b", new(big.Rat).SetInt64(10)),
				NewShare("c", new(big.Rat).SetInt64(15)),
				NewShare("d", new(big.Rat).SetInt64(20.0)),
				NewShare("e", new(big.Rat).SetInt64(25.0)),
			},
			output: map[string]*big.Rat{
				"a": new(big.Rat).SetFrac64(5, 75),
				"b": new(big.Rat).SetFrac64(10, 75),
				"c": new(big.Rat).SetFrac64(15, 75),
				"d": new(big.Rat).SetFrac64(20, 75),
				"e": new(big.Rat).SetFrac64(25, 75),
			},
			err: nil,
		},
		"zero shares": {
			input: []*Share{
				NewShare("a", new(big.Rat)),
				NewShare("b", new(big.Rat)),
				NewShare("c", new(big.Rat)),
				NewShare("d", new(big.Rat)),
				NewShare("e", new(big.Rat)),
			},
			output: nil,
			err:    ErrDivideByZero(),
		},
	}

	for name, test := range set {
		actual, err := CalculateDividends(test.input)

		if err != test.err {
			errValue := ""
			expectedValue := ""

			if err != nil {
				errValue = err.Error()
			}

			if test.err != nil {
				expectedValue = test.err.Error()
			}

			if errValue != expectedValue {
				t.Errorf("(%s): error generated was (%v), expected (%v).",
					name, errValue, expectedValue)
			}
		}

		for stakeholder, dividend := range test.output {
			if actual[stakeholder].Cmp(dividend) != 0 {
				t.Errorf("(%s): stakeholder (%v) dividend was (%v), "+
					"expected (%v).", name, stakeholder, actual[stakeholder],
					dividend)
			}
		}
	}
}
func TestCalculatePoolTarget(t *testing.T) {
	set := []struct {
		hashRate   *big.Int
		targetTime *big.Int
		expected   string
	}{
		{
			new(big.Int).SetInt64(1.2E12),
			new(big.Int).SetInt64(15),
			"1381437475988024283268563409791075016144953" +
				"2887812045339949604391304358105",
		},
		{
			new(big.Int).SetInt64(1.2E12),
			new(big.Int).SetInt64(10),
			"2072156213982036424902845114686612524217429" +
				"9331718068009924406586956537158",
		},
	}

	for _, test := range set {
		target := CalculatePoolTarget(test.hashRate, test.targetTime)
		expected, success := new(big.Int).SetString(test.expected, 10)
		if !success {
			t.Errorf("Failed to parse (%v) as a big.Int", test.expected)
		}

		if target.Cmp(expected) != 0 {
			t.Errorf("For a hashrate of (%v) and a target time of (%v) the "+
				"expected target is (%v), got (%v).", test.hashRate,
				test.targetTime, expected, target)
		}
	}
}
