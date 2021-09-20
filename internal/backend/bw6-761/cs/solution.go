// Copyright 2020 ConsenSys Software Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by gnark DO NOT EDIT

package cs

import (
	"errors"
	"fmt"
	"io"

	"github.com/consensys/gnark/backend/hint"
	"github.com/consensys/gnark/internal/backend/compiled"

	"github.com/consensys/gnark-crypto/ecc/bw6-761/fr"
)

// ErrUnsatisfiedConstraint can be generated when solving a R1CS
var ErrUnsatisfiedConstraint = errors.New("constraint is not satisfied")

// solution represents elements needed to compute
// a solution to a R1CS or SparseR1CS
type solution struct {
	values, coefficients []fr.Element
	solved               []bool
	nbSolved             int
	mHintsFunctions      map[hint.ID]hintFunction
}

func newSolution(nbWires int, hintFunctions []hint.Function, coefficients []fr.Element) (solution, error) {
	s := solution{
		values:          make([]fr.Element, nbWires),
		coefficients:    coefficients,
		solved:          make([]bool, nbWires),
		mHintsFunctions: make(map[hint.ID]hintFunction, len(hintFunctions)+2),
	}

	s.mHintsFunctions = make(map[hint.ID]hintFunction, len(hintFunctions)+2)
	s.mHintsFunctions[hint.IsZero] = powModulusMinusOne
	s.mHintsFunctions[hint.IthBit] = ithBit

	for i := 0; i < len(hintFunctions); i++ {
		if _, ok := s.mHintsFunctions[hintFunctions[i].ID]; ok {
			return solution{}, fmt.Errorf("duplicate hint function with id %d", uint32(hintFunctions[i].ID))
		}
		f, ok := hintFunctions[i].F.(hintFunction)
		if !ok {
			return solution{}, fmt.Errorf("invalid hint function signature with id %d", uint32(hintFunctions[i].ID))
		}
		s.mHintsFunctions[hintFunctions[i].ID] = f
	}

	return s, nil
}

func (s *solution) set(id int, value fr.Element) {
	if s.solved[id] {
		panic("solving the same wire twice should never happen.")
	}
	s.values[id] = value
	s.solved[id] = true
	s.nbSolved++
}

func (s *solution) isValid() bool {
	return s.nbSolved == len(s.values)
}

// computeTerm computes coef*variable
func (s *solution) computeTerm(t compiled.Term) fr.Element {
	cID, vID, _ := t.Unpack()
	if cID != 0 && !s.solved[vID] {
		panic("computing a term with an unsolved wire")
	}
	switch cID {
	case compiled.CoeffIdZero:
		return fr.Element{}
	case compiled.CoeffIdOne:
		return s.values[vID]
	case compiled.CoeffIdTwo:
		var res fr.Element
		res.Double(&s.values[vID])
		return res
	case compiled.CoeffIdMinusOne:
		var res fr.Element
		res.Neg(&s.values[vID])
		return res
	default:
		var res fr.Element
		res.Mul(&s.coefficients[cID], &s.values[vID])
		return res
	}
}

// solveHint compute solution.values[vID] using provided solver hint
func (s *solution) solveWithHint(vID int, h compiled.Hint) error {
	// compute values for all inputs.
	inputs := make([]fr.Element, len(h.Inputs))

	for i := 0; i < len(h.Inputs); i++ {
		// input is a linear expression, we must compute the value
		for j := 0; j < len(h.Inputs[i]); j++ {
			ciID, viID, visibility := h.Inputs[i][j].Unpack()
			if visibility == compiled.Virtual {
				// we have a constant, just take the coefficient value
				inputs[i].Add(&inputs[i], &s.coefficients[ciID])
				continue
			}
			if !s.solved[viID] {
				return errors.New("expected wire to be instantiated while evaluating hint")
			}
			v := s.computeTerm(h.Inputs[i][j])
			inputs[i].Add(&inputs[i], &v)
		}
	}

	f, ok := s.mHintsFunctions[h.ID]
	if !ok {
		return errors.New("missing hint function")
	}
	v, err := f(inputs)
	if err != nil {
		return err
	}
	s.set(vID, v)
	return nil
}

func (s *solution) printLogs(w io.Writer, logs []compiled.LogEntry) {
	if w == nil {
		return
	}

	for i := 0; i < len(logs); i++ {
		logLine := s.logValue(logs[i])
		_, _ = io.WriteString(w, logLine)
	}
}

const unsolvedVariable = "<unsolved>"

func (s *solution) logValue(log compiled.LogEntry) string {
	var toResolve []interface{}
	var (
		isEval       bool
		eval         fr.Element
		missingValue bool
	)
	for j := 0; j < len(log.ToResolve); j++ {
		if log.ToResolve[j] == compiled.TermDelimitor {
			// this is a special case where we want to evaluate the following terms until the next delimitor.
			if !isEval {
				isEval = true
				missingValue = false
				eval.SetZero()
				continue
			}
			isEval = false
			if missingValue {
				toResolve = append(toResolve, unsolvedVariable)
			} else {
				// we have to append our accumulator
				toResolve = append(toResolve, eval.String())
			}
			continue
		}
		cID, vID, visibility := log.ToResolve[j].Unpack()

		if isEval {
			// we are evaluating
			if visibility == compiled.Virtual {
				// just add the constant
				eval.Add(&eval, &s.coefficients[cID])
				continue
			}
			if !s.solved[vID] {
				missingValue = true
				continue
			}
			tv := s.computeTerm(log.ToResolve[j])
			eval.Add(&eval, &tv)
			continue
		}

		if visibility == compiled.Virtual {
			// it's just a constant
			if cID == compiled.CoeffIdMinusOne {
				toResolve = append(toResolve, "-1")
			} else {
				toResolve = append(toResolve, s.coefficients[cID].String())
			}
			continue
		}
		if !(cID == compiled.CoeffIdMinusOne || cID == compiled.CoeffIdOne) {
			toResolve = append(toResolve, s.coefficients[cID].String())
		}
		if !s.solved[vID] {
			toResolve = append(toResolve, unsolvedVariable)
		} else {
			toResolve = append(toResolve, s.values[vID].String())
		}
	}
	return fmt.Sprintf(log.Format, toResolve...)
}
