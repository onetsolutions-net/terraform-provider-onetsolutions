package common

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// AttrTimeouts is the name of the timeouts block.
const AttrTimeouts = "timeouts"

// Timeouts lists the default duration of each operation a resource bounds; a zero duration
// leaves the operation out of the block.
type Timeouts struct {
	Create time.Duration
	Update time.Duration
	Delete time.Duration
}

// Operation names one bounded operation.
type Operation int

// Bounded operations.
const (
	OpCreate Operation = iota
	OpUpdate
	OpDelete
)

// TimeoutsBlock returns the `timeouts` block, documenting each default.
func TimeoutsBlock(ctx context.Context, defaults Timeouts) schema.Block {
	describe := func(op string, d time.Duration) string {
		return fmt.Sprintf("How long to wait for the %s, as a duration such as \"30s\" or \"2h45m\". Defaults to %#q.", op, formatDuration(d))
	}
	return timeouts.Block(ctx, timeouts.Opts{
		Create:            defaults.Create > 0,
		Update:            defaults.Update > 0,
		Delete:            defaults.Delete > 0,
		CreateDescription: describe("creation", defaults.Create),
		UpdateDescription: describe("update", defaults.Update),
		DeleteDescription: describe("deletion", defaults.Delete),
	})
}

// WithTimeout returns ctx bounded by the configured timeout of op, or by its default.
func WithTimeout(ctx context.Context, value timeouts.Value, op Operation, defaults Timeouts) (context.Context, context.CancelFunc, diag.Diagnostics) {
	var (
		d     time.Duration
		diags diag.Diagnostics
	)
	switch op {
	case OpCreate:
		d, diags = value.Create(ctx, defaults.Create)
	case OpUpdate:
		d, diags = value.Update(ctx, defaults.Update)
	case OpDelete:
		d, diags = value.Delete(ctx, defaults.Delete)
	}
	if diags.HasError() {
		return ctx, func() {}, diags
	}
	bounded, cancel := context.WithTimeout(ctx, d)
	return bounded, cancel, diags
}

func formatDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}
