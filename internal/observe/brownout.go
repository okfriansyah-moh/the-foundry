package observe

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// BrownoutMode reports whether brownout mode is currently active (1) or
// not (0) — a gauge rather than a counter because it is a current-state
// flag, matching queue_depth's own shape (Task 31).
var BrownoutMode = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "foundry_brownout_mode",
	Help: "1 when brownout mode is active (lowest-priority lanes shed), 0 otherwise (docs/PLAN.md Task 33).",
})

func init() {
	Registry.MustRegister(BrownoutMode)
}

// BrownoutController is the brownout mode flag docs/PLAN.md Task 33's
// Steps name: a single on/off switch that, once enabled, stops admitting
// work on whichever lanes config/queue-priority.yaml marks sheddable
// (learning today) while every non-sheddable lane (recovery, delivery,
// notification) keeps being admitted — "sheds learning/memory queues
// first, keeps delivery+recovery."
type BrownoutController struct {
	enabled   atomic.Bool
	sheddable map[Lane]bool
}

// NewBrownoutController builds a BrownoutController from cfg's per-lane
// Sheddable flags, starting disabled (every lane admitted).
func NewBrownoutController(cfg QueueConfig) *BrownoutController {
	sheddable := make(map[Lane]bool, len(cfg.Lanes))
	for _, l := range cfg.Lanes {
		sheddable[Lane(l.Name)] = l.Sheddable
	}
	return &BrownoutController{sheddable: sheddable}
}

// Enabled reports whether brownout mode is currently active.
func (b *BrownoutController) Enabled() bool {
	return b.enabled.Load()
}

// SetEnabled turns brownout mode on or off and updates the BrownoutMode
// gauge to match.
func (b *BrownoutController) SetEnabled(v bool) {
	b.enabled.Store(v)
	if v {
		BrownoutMode.Set(1)
	} else {
		BrownoutMode.Set(0)
	}
}

// Admit reports whether lane should currently accept new work: always
// true while brownout mode is disabled; while enabled, true only for
// lanes this controller was not configured as sheddable. A lane this
// controller has no configuration for (a name outside the four known
// lanes) is treated as non-sheddable — the fail-safe default is to keep
// admitting unknown work rather than silently drop it.
func (b *BrownoutController) Admit(lane Lane) bool {
	if !b.Enabled() {
		return true
	}
	return !b.sheddable[lane]
}
