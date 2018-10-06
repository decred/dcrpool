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
