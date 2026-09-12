package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

func WriteReport(writer io.Writer, report Report, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	if len(report.Endpoints) == 1 {
		fmt.Fprintln(writer, "Note: add at least one custom feed to measure relative first-arrival latency.")
	}
	fmt.Fprintf(writer, "Duration: %.1fs | unique events: %d | common events: %d\n\n", report.DurationSec, report.UniqueEvents, report.CommonEvents)
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "RANK\tFEED\tSTATUS\tEVENTS\tRATE\tCOVERAGE\tMATCHED\tWIN RATE\tP50 LAG\tP95 LAG\tP99 LAG\tFEED AGE\tDISCONNECTS"); err != nil {
		return err
	}
	for _, item := range report.Endpoints {
		status := "down"
		if item.Connected {
			status = "connected"
			if item.LastObservation != "" {
				status = "live"
			}
		}
		if _, err := fmt.Fprintf(table, "%d\t%s\t%s\t%d\t%.1f/s\t%.2f%%\t%d\t%.2f%%\t%s\t%s\t%s\t%s\t%d\n",
			item.Rank, item.Name, status, item.Observed, item.EventsPerSec, item.CoveragePct, item.Matched,
			item.WinRatePct, milliseconds(item.P50LagMS), milliseconds(item.P95LagMS),
			milliseconds(item.P99LagMS), milliseconds(item.MedianFeedAgeMS), item.Disconnects); err != nil {
			return err
		}
	}
	return table.Flush()
}

func milliseconds(value float64) string {
	return (time.Duration(value * float64(time.Millisecond))).String()
}
