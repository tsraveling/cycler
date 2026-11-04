package main

import "time"

// @region block:resolve -- TODAY'S BLOCKS, OVERRIDES, BOUNDARIES

// a block as today actually has it, after the day's drift is applied
type resolvedBlock struct {
	block
	start     int
	end       int
	cancelled bool
}

// minutes from midnight
func minuteOf(t time.Time) int { return t.Hour()*60 + t.Minute() }

func (s *dayState) override(id string) blockOverride {
	if o, ok := s.Blocks[id]; ok {
		return o
	}
	return blockOverride{Start: -1, End: -1}
}

func (s *dayState) setOverride(id string, fn func(*blockOverride)) {
	o := s.override(id)
	fn(&o)
	if s.Blocks == nil {
		s.Blocks = map[string]blockOverride{}
	}
	s.Blocks[id] = o
}

func (s *dayState) dropBlockOverride(id string) {
	delete(s.Blocks, id)
	if s.Held == id {
		s.Held = ""
	}
}

// today's blocks in start order, with the day's drift applied. cancelled blocks
// stay in the slice because the breadcrumb still shows them; every selection
// path skips them.
func (s *dayState) dayBlocks(c *config) []resolvedBlock {
	wd := s.weekday()
	out := make([]resolvedBlock, 0, len(c.Blocks))
	for _, b := range c.Blocks {
		if !b.activeOn(wd) {
			continue
		}
		o := s.override(b.ID)
		r := resolvedBlock{block: b, start: b.Start, end: b.End, cancelled: o.Cancelled}
		if o.Start >= 0 {
			r.start = o.Start
		}
		if o.End >= 0 {
			r.end = o.End
		}
		if r.end <= r.start {
			r.cancelled = true
		}
		out = append(out, r)
	}
	return out
}

// moves the day's boundary to minute `at`, from fromIdx onward: a block that
// ends at or before `at` is consumed and cancelled, and the first block
// surviving it starts at `at`. no block's end is touched.
func (s *dayState) cascadeFrom(c *config, fromIdx, at int) {
	bs := s.dayBlocks(c)
	for i := fromIdx; i < len(bs); i++ {
		b := bs[i]
		if b.cancelled {
			continue
		}
		if b.end <= at {
			s.setOverride(b.ID, func(o *blockOverride) { o.Cancelled = true })
			continue
		}
		// unconditional: the boundary pulls a block forward as readily as it
		// pushes one back
		s.setOverride(b.ID, func(o *blockOverride) { o.Start = at })
		return
	}
}

// ends block idx at `at` and cascades into what follows. the one mutation
// behind extend, finish-early and the overrun release.
func (s *dayState) endBlockAt(c *config, idx, at int) {
	bs := s.dayBlocks(c)
	if idx < 0 || idx >= len(bs) {
		return
	}
	s.setOverride(bs[idx].ID, func(o *blockOverride) { o.End = at })
	s.cascadeFrom(c, idx+1, at)
}

// what cascadeFrom would cancel, without mutating anything. drives the extend
// control's warning.
func (s *dayState) wouldCancel(c *config, fromIdx, at int) []string {
	bs := s.dayBlocks(c)
	var out []string
	for i := fromIdx; i < len(bs); i++ {
		b := bs[i]
		if b.cancelled {
			continue
		}
		if b.end <= at {
			out = append(out, b.Title)
			continue
		}
		break
	}
	return out
}

// @region block:phase -- WHAT THE DAY IS DOING RIGHT NOW

type dayPhase int

const (
	// no blocks run today at all
	phaseNoBlocks dayPhase = iota
	// before the first block
	phaseSleep
	// inside a block
	phaseBlock
	// between two blocks
	phaseGap
	// past the last block
	phaseDone
)

// the phase, the block it concerns, and that block's index in dayBlocks. for
// sleep and gap the block is the one coming up; for done it is the last one.
func (m model) phase() (dayPhase, resolvedBlock, int) {
	bs := m.s.dayBlocks(m.c)
	live := -1
	for i, b := range bs {
		if !b.cancelled {
			live = i
			break
		}
	}
	if live < 0 {
		return phaseNoBlocks, resolvedBlock{}, -1
	}

	if m.s.Finished {
		for i := len(bs) - 1; i >= 0; i-- {
			if !bs[i].cancelled {
				return phaseDone, bs[i], i
			}
		}
	}

	// a held cycler block outranks the clock until its item is finished
	if m.s.Held != "" {
		for i, b := range bs {
			if b.ID == m.s.Held && !b.cancelled {
				return phaseBlock, b, i
			}
		}
	}

	now := minuteOf(nowFn())
	last := -1
	for i, b := range bs {
		if b.cancelled {
			continue
		}
		if now < b.start {
			if last < 0 {
				return phaseSleep, b, i
			}
			return phaseGap, b, i
		}
		if now < b.end {
			return phaseBlock, b, i
		}
		last = i
	}
	return phaseDone, bs[last], last
}

// the crumb the trail scrolls to keep in view
func phaseAnchor(ph dayPhase, idx int) int {
	if idx < 0 {
		return 0
	}
	return idx
}

// is this phase showing the cycler rather than a clock
func (m model) inCyclerBlock() bool {
	ph, b, _ := m.phase()
	return ph == phaseBlock && b.Type == blkCycler
}

// no blocks configured at all means the app behaves as it did before blocks
// existed; blocks configured but none today is an all-day gap
func (m model) blocksOff() bool { return len(m.c.Blocks) == 0 }
