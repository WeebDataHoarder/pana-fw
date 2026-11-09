package main

import (
	"errors"
	"fmt"
	"slices"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/lang"
)

type LangConfigEntry struct {
	// Selectors, first by Id if non-zero, then Label if not empty, and if none specified use Match

	Id    int    `yaml:"id"`
	Label string `yaml:"label"`

	Strings map[int]string `yaml:"strings"`

	// Match optional matches to filter entries
	Match map[int]string `yaml:"match"`
}

func (e LangConfigEntry) Patch(f *lang.File) error {
	var targets []*lang.Record

	if e.Id != 0 {
		if r := f.RecordById(e.Id); r != nil {
			targets = append(targets, r)
		} else {
			return fmt.Errorf("no such record: %d", e.Id)
		}
	}
	if e.Label != "" {
		if targets == nil {
			targets = f.RecordsByLabel(e.Label)
		} else {
			// filter
			for i, target := range slices.Backward(targets) {
				if target.Label != e.Label {
					targets = slices.Delete(targets, i, 1)
				}
			}
		}
		if len(targets) == 0 {
			return fmt.Errorf("no such label: %s", e.Label)
		}
	}

	if len(targets) == 0 {
		// allow directly matching strings, slow
		var matched bool
		if len(e.Match) > 0 {
			for i, v := range e.Strings {
				if m, ok := e.Match[i]; ok {
					for j := 0; j < len(*f); j++ {
						r := &(*f)[j]
						if str := r.CellString(i); str == m {
							if err := r.SetCellString(i, v); err != nil {
								return err
							}
							matched = true
						}
					}
				} else {
					return fmt.Errorf("no match specified for: %s", v)
				}
			}
		}
		if matched {
			return nil
		}
		return errors.New("no target matched")
	}

	for i, v := range e.Strings {
		if m, ok := e.Match[i]; ok {
			for _, t := range targets {
				if str := t.CellString(i); str == m {
					if err := t.SetCellString(i, v); err != nil {
						return err
					}
				}
			}
		} else {
			for _, t := range targets {
				if err := t.SetCellString(i, v); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
