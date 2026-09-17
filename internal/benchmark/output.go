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
	return WriteGrpcStyleReport(writer, report)
}

// WriteGrpcStyleReport mirrors grpc-benchmark's end-of-run summary.
func WriteGrpcStyleReport(writer io.Writer, report Report) error {
	fmt.Fprintln(writer, "✓ 测试完成，正在分析结果...")
	fmt.Fprintln(writer, "────────────────────────────────────────────────────────────────────────────────")

	for _, item := range report.Endpoints {
		fmt.Fprintf(writer, "\n📊 %s 性能分析\n", item.Name)
		fmt.Fprintln(writer, "------------------------------")
		fmt.Fprintf(writer, "总接收事件数: %s events\n", bright(fmt.Sprintf("%d", item.Observed)))
		firstPct := item.WinRatePct
		fmt.Fprintf(writer, "首先接收事件数: %s events\n",
			bright(fmt.Sprintf("%d (%.2f%%)", item.Wins, firstPct)))
		behind := item.Matched - item.Wins
		behindPct := 0.0
		if item.Matched > 0 {
			behindPct = float64(behind) * 100 / float64(item.Matched)
		}
		fmt.Fprintf(writer, "落后接收事件数: %s events\n",
			bright(fmt.Sprintf("%d (%.2f%%)", behind, behindPct)))
		if behind > 0 {
			fmt.Fprintln(writer, "ℹ 延迟统计 (相对于最快端点):")
			fmt.Fprintf(writer, "  平均延迟: %s ms\n", bright(fmt.Sprintf("%.2f", item.BehindMeanLagMS)))
			fmt.Fprintf(writer, "  P50 延迟: %s ms\n", bright(fmt.Sprintf("%.2f", item.P50LagMS)))
			fmt.Fprintf(writer, "  P95 延迟: %s ms\n", bright(fmt.Sprintf("%.2f", item.P95LagMS)))
			fmt.Fprintf(writer, "  P99 延迟: %s ms\n", bright(fmt.Sprintf("%.2f", item.P99LagMS)))
			fmt.Fprintf(writer, "  最大延迟: %s ms\n", bright(fmt.Sprintf("%.2f", item.MaxLagMS)))
		} else if item.Matched > 0 {
			fmt.Fprintln(writer, "✓ 该端点始终是最快的，没有延迟数据")
		} else {
			fmt.Fprintln(writer, "⚠ 没有收集到公共对比样本")
		}
		fmt.Fprintln(writer, "────────────────────────────────────────────────────────────────────────────────")
	}

	fmt.Fprintln(writer, "🏆 端点性能对比")
	fmt.Fprintln(writer, "----------------------------")
	for _, item := range report.Endpoints {
		fmt.Fprintf(writer,
			"%-12s: 首先接收 %6.2f%%, 落后时平均延迟 %6.2fms, 总体平均延迟 %6.2fms\n",
			item.Name, item.WinRatePct, item.BehindMeanLagMS, item.MeanLagMS)
	}
	fmt.Fprintln(writer, "────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(writer, "Duration: %.1fs | unique events: %d | common events: %d\n\n",
		report.DurationSec, report.UniqueEvents, report.CommonEvents)

	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "RANK\tFEED\tSTATUS\tEVENTS\tRATE\tCOVERAGE\tMATCHED\tWIN RATE\tP50 LAG\tP95 LAG\tP99 LAG\tFEED AGE\tDISCONNECTS")
	for _, item := range report.Endpoints {
		status := "down"
		if item.Connected {
			status = "connected"
			if item.LastObservation != "" {
				status = "live"
			}
		}
		fmt.Fprintf(table, "%d\t%s\t%s\t%d\t%.1f/s\t%.2f%%\t%d\t%.2f%%\t%s\t%s\t%s\t%s\t%d\n",
			item.Rank, item.Name, status, item.Observed, item.EventsPerSec, item.CoveragePct, item.Matched,
			item.WinRatePct, milliseconds(item.P50LagMS), milliseconds(item.P95LagMS),
			milliseconds(item.P99LagMS), milliseconds(item.MedianFeedAgeMS), item.Disconnects)
	}
	return table.Flush()
}

func bright(value string) string {
	return value
}

func milliseconds(value float64) string {
	return (time.Duration(value * float64(time.Millisecond))).String()
}
