package app

import (
	"math"
	"testing"
	"time"
)

func TestParseDatabaseTimeTreatsTimestampWithoutZoneAsUTC(t *testing.T) {
	got, err := parseDatabaseTime("2026-07-11 18:30:00")
	if err != nil {
		t.Fatalf("parse timestamp: %v", err)
	}
	if got.Location() != time.UTC || got.In(jakartaLocation).Format("2006-01-02") != "2026-07-12" {
		t.Fatalf("expected UTC instant mapping to Jakarta next day, got %v (%s)", got, got.Location())
	}
}

func TestLoanOverdueIncludesPartiallyUnpaidOldestInstallment(t *testing.T) {
	yesterday := time.Now().In(jakartaLocation).AddDate(0, 0, -1).Format("2006-01-02")
	if !loanOverdue(yesterday, 1) {
		t.Fatal("expected a positive partial balance after the oldest due date to be overdue")
	}
	if loanOverdue(yesterday, 0) {
		t.Fatal("expected a fully paid installment not to be overdue")
	}
}

func TestCalculateLoanScheduleClampsExactMonthlyDeadlines(t *testing.T) {
	schedule, err := calculateLoanSchedule(1_000_000, 4, 100, "2024-01-31")
	if err != nil {
		t.Fatal(err)
	}
	wantDates := []string{"2024-02-29", "2024-03-31", "2024-04-30", "2024-05-31"}
	var sum int64
	for index, installment := range schedule.Installments {
		if installment.DueDate != wantDates[index] {
			t.Fatalf("installment %d date = %s, want %s", index+1, installment.DueDate, wantDates[index])
		}
		sum += installment.ScheduledAmount
	}
	if schedule.TotalInterest != 40_000 || schedule.TotalObligation != 1_040_000 || sum != schedule.TotalObligation {
		t.Fatalf("unexpected schedule totals: %+v (sum %d)", schedule, sum)
	}
}

func TestCalculateLoanSchedulePutsRemainderInFinalInstallment(t *testing.T) {
	schedule, err := calculateLoanSchedule(100, 3, 0, "2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{33, 33, 34}
	for index, installment := range schedule.Installments {
		if installment.ScheduledAmount != want[index] {
			t.Fatalf("installment %d = %d, want %d", index+1, installment.ScheduledAmount, want[index])
		}
	}
}

func TestCalculateLoanScheduleValidatesInputs(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal int64
		duration  int
		rate      int
		date      string
	}{
		{"zero principal", 0, 1, 100, "2026-01-01"},
		{"zero duration", 100, 0, 100, "2026-01-01"},
		{"excess duration", math.MaxInt64, maxLoanDurationMonths + 1, 1000, "2026-01-01"},
		{"negative rate", 100, 1, -1, "2026-01-01"},
		{"excess rate", 100, 1, 1001, "2026-01-01"},
		{"invalid date", 100, 1, 100, "2026-02-30"},
		{"obligation below duration", 1, 2, 0, "2026-01-01"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := calculateLoanSchedule(test.principal, test.duration, test.rate, test.date); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseInterestRatePercentExactHundredths(t *testing.T) {
	for input, want := range map[string]int{"0": 0, "1": 100, "1.25": 125, "10.00": 1000, "0.01": 1} {
		got, err := parseInterestRatePercent(input)
		if err != nil || got != want {
			t.Fatalf("parse %q = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "-1", "10.01", "11", "1.001", "one", ".5", "999999999999999999999999999999999999", "18446744073709551615.99"} {
		if _, err := parseInterestRatePercent(input); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}

func TestAllocateRepaymentsOldestFirst(t *testing.T) {
	installments := []InstallmentCalculation{{ScheduledAmount: 100}, {ScheduledAmount: 100}, {ScheduledAmount: 100}}
	total, err := allocateRepaymentsOldestFirst(installments, []int64{50, 175})
	if err != nil {
		t.Fatal(err)
	}
	if total != 225 || installments[0].PaidAmount != 100 || installments[1].PaidAmount != 100 || installments[2].PaidAmount != 25 {
		t.Fatalf("unexpected allocation: total=%d installments=%+v", total, installments)
	}
}

func TestAllocateRepaymentsRejectsOverpayment(t *testing.T) {
	installments := []InstallmentCalculation{{ScheduledAmount: 100}}
	if _, err := allocateRepaymentsOldestFirst(installments, []int64{101}); err == nil {
		t.Fatal("expected overpayment error")
	}
	if installments[0].PaidAmount != 0 {
		t.Fatalf("failed allocation mutated installment: %+v", installments[0])
	}
}

func TestCalculateLoanScheduleRateBoundariesAndNonLeapClamp(t *testing.T) {
	zero, err := calculateLoanSchedule(10_000, 1, 0, "2023-01-31")
	if err != nil || zero.TotalInterest != 0 || zero.Installments[0].DueDate != "2023-02-28" {
		t.Fatalf("zero-rate schedule = %+v, err=%v", zero, err)
	}
	maximum, err := calculateLoanSchedule(10_000, 1, 1000, "2023-01-31")
	if err != nil || maximum.TotalInterest != 1_000 {
		t.Fatalf("maximum-rate schedule = %+v, err=%v", maximum, err)
	}
}
