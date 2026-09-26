package main

// Usage:
//
// go run cmd/vad_diff/main.go speech_segs_run1.2.txt speech_segs_lowmem_run3.txt

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Segment struct {
	start, end int
}

func readSegments(filename string) ([]Segment, error) {
	fmt.Printf("reading segments from %s\n", filename)
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var result []Segment
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "detected") {
			continue
		}
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			fmt.Printf("unexpected part count: %d\n", len(parts))
			return nil, errors.New("unexpected part count")
		}
		a64, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return nil, err
		}
		b64, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, err
		}
		result = append(result, Segment{
			start: int(a64 * 1000),
			end:   int(b64 * 1000),
		})
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("no segments found")
	}
	return result, nil
}

func getEnd(segs []Segment) int {
	if len(segs) == 0 {
		return 0
	}
	return segs[len(segs)-1].end
}

func fillSlots(segs []Segment, slotSize int) []bool {
	slots := make([]bool, slotSize)
	for _, seg := range segs {
		for i := seg.start; i <= seg.end; i++ {
			slots[i] = true
		}
	}
	return slots
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("got %d args instead of 3", len(os.Args))
	}
	before, err := readSegments(os.Args[1])
	if err != nil {
		return err
	}
	beforeEnd := getEnd(before)
	fmt.Printf("read %d segments before: %d - %d...%d\n", len(before), before[0].start, before[0].end, beforeEnd)

	after, err := readSegments(os.Args[2])
	if err != nil {
		return err
	}
	afterEnd := getEnd(after)
	fmt.Printf("read %d segments after: %d - %d...%d\n", len(after), after[0].start, after[0].end, afterEnd)

	slotSize := max(beforeEnd, afterEnd) + 1

	slotsBefore := fillSlots(before, slotSize)
	slotsAfter := fillSlots(after, slotSize)

	common := 0
	onlyb := 0
	onlya := 0

	onlyb_stretch := 0
	onlya_stretch := 0

	max_onlyb := 0
	max_onlyb_pos := -1
	max_onlya := 0
	max_onlya_pos := -1

	for i := range slotSize {
		if slotsBefore[i] {
			if slotsAfter[i] {
				common++
				onlyb_stretch = 0
				onlya_stretch = 0
			} else {
				onlyb++
				onlya_stretch = 0
				onlyb_stretch++
				if onlyb_stretch > max_onlyb {
					max_onlyb = onlyb_stretch
					max_onlyb_pos = i
				}
			}
		} else if slotsAfter[i] {
			onlya++
			onlyb_stretch = 0
			onlya_stretch++
			if onlya_stretch > max_onlya {
				max_onlya = onlya_stretch
				max_onlya_pos = i
			}
		} else {
			onlyb_stretch = 0
			onlya_stretch = 0
		}
	}

	fmt.Printf("common %d, only before %d, only after %d\n", common, onlyb, onlya)
	if max_onlyb_pos >= 0 {
		fmt.Printf("max only before stretch: len %d, %d - %d ms\n", max_onlyb, max_onlyb_pos-max_onlyb+1, max_onlyb_pos)
	}
	if max_onlya_pos >= 0 {
		fmt.Printf("max only after stretch: len %d, %d - %d ms\n", max_onlya, max_onlya_pos-max_onlya+1, max_onlya_pos)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("run fail: %s", err.Error())
	}
}
