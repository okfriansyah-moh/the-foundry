package evolve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var budgetReservationKeyPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type durableBudgetState struct {
	Events []budgetReservation
	Window BudgetWindow
}

type budgetReservation struct {
	Sequence     int       `json:"sequence"`
	Timestamp    time.Time `json:"timestamp"`
	Key          string    `json:"key"`
	Action       string    `json:"action"`
	FilesChanged int       `json:"files_changed"`
}

func loadDurableBudget(root *os.Root, now time.Time) (durableBudgetState, error) {
	if info, err := root.Lstat(evolutionBudgetRecords); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return durableBudgetState{}, fmt.Errorf("evolve: budget records path is a symlink or non-directory")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return durableBudgetState{Events: []budgetReservation{}}, nil
	} else if err != nil {
		return durableBudgetState{}, fmt.Errorf("evolve: inspect budget records: %w", err)
	}
	directory, err := openRealDirectory(root, evolutionBudgetRecords)
	if err != nil {
		return durableBudgetState{}, fmt.Errorf("evolve: open budget records: %w", err)
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return durableBudgetState{}, errors.Join(readErr, closeErr)
	}
	sort.Strings(names)
	state := durableBudgetState{Events: make([]budgetReservation, 0, len(names))}
	seenKeys := make(map[string]struct{}, len(names))
	cutoff := now.AddDate(0, 0, -30)
	var lastTimestamp time.Time
	for index, name := range names {
		want := fmt.Sprintf("%020d.json", index+1)
		if name != want {
			return durableBudgetState{}, fmt.Errorf("evolve: non-contiguous budget record %q, want %q", name, want)
		}
		raw, err := readImmutableObject(root, filepath.ToSlash(filepath.Join(evolutionBudgetRecords, name)))
		if err != nil {
			return durableBudgetState{}, err
		}
		var event budgetReservation
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return durableBudgetState{}, fmt.Errorf("evolve: decode budget record %s: %w", name, err)
		}
		canonical, _ := json.Marshal(event)
		canonical = append(canonical, '\n')
		if !bytes.Equal(canonical, raw) || event.Sequence != index+1 || event.Timestamp.IsZero() || event.Timestamp.Location() != time.UTC || event.Timestamp.After(now) || (!lastTimestamp.IsZero() && event.Timestamp.Before(lastTimestamp)) || !budgetReservationKeyPattern.MatchString(event.Key) || event.FilesChanged <= 0 || (event.Action != "promote" && event.Action != "rollback") {
			return durableBudgetState{}, fmt.Errorf("evolve: invalid canonical budget record %s", name)
		}
		if _, duplicate := seenKeys[event.Key]; duplicate {
			return durableBudgetState{}, fmt.Errorf("evolve: duplicate budget reservation key")
		}
		seenKeys[event.Key] = struct{}{}
		lastTimestamp = event.Timestamp
		state.Events = append(state.Events, event)
		if event.Timestamp.After(cutoff) {
			applyBudgetEvent(&state.Window, event)
		}
	}
	return state, nil
}

func reserveDurableBudget(root *os.Root, state *durableBudgetState, limits ChangeBudgetLimits, reservation budgetReservation) (FreezeCondition, error) {
	if reservation.Timestamp.IsZero() || reservation.Timestamp.Location() != time.UTC || !budgetReservationKeyPattern.MatchString(reservation.Key) || reservation.FilesChanged <= 0 || (reservation.Action != "promote" && reservation.Action != "rollback") {
		return "", fmt.Errorf("evolve: invalid budget reservation")
	}
	if len(state.Events) > 0 && reservation.Timestamp.Before(state.Events[len(state.Events)-1].Timestamp) {
		return "", fmt.Errorf("evolve: budget reservation timestamp moves backwards")
	}
	for _, existing := range state.Events {
		if existing.Key == reservation.Key {
			return "", nil
		}
	}
	reservation.Sequence = len(state.Events) + 1
	projected := state.Window
	applyBudgetEvent(&projected, reservation)
	var breach FreezeCondition
	if breaches := projected.Breaches(limits); len(breaches) != 0 {
		breach = breaches[0]
		if reservation.Action != "rollback" {
			return breach, nil
		}
	}
	if err := ensureDirectories(root, evolutionBudgetRecords); err != nil {
		return "", err
	}
	raw, err := json.Marshal(reservation)
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	path := filepath.ToSlash(filepath.Join(evolutionBudgetRecords, fmt.Sprintf("%020d.json", reservation.Sequence)))
	if err := writeImmutable(root, path, raw); err != nil {
		return "", fmt.Errorf("evolve: persist budget reservation: %w", err)
	}
	state.Events = append(state.Events, reservation)
	state.Window = projected
	return breach, nil
}

func mergeBudgetWindow(window BudgetWindow, state durableBudgetState) BudgetWindow {
	window.Promotions = state.Window.Promotions
	window.FilesChanged = state.Window.FilesChanged
	window.RollbackChainDepth = state.Window.RollbackChainDepth
	return window
}

func applyBudgetEvent(window *BudgetWindow, event budgetReservation) {
	window.FilesChanged += event.FilesChanged
	if event.Action == "promote" {
		window.Promotions++
		window.RollbackChainDepth = 0
	} else {
		window.RollbackChainDepth++
	}
}
