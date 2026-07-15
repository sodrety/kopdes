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

func TestCalculateRegularLoanScheduleUsesTieredMonthlyAdminFee(t *testing.T) {
	calculation, err := calculateRegularLoanSchedule(30_000_000, 24, "2026-01-31")
	if err != nil {
		t.Fatal(err)
	}
	if calculation.MonthlyAdminFee != 325_000 || calculation.TotalAdminFee != 7_800_000 || calculation.TotalObligation != 37_800_000 {
		t.Fatalf("unexpected Regular Loan calculation: %+v", calculation)
	}
	var scheduled int64
	for _, installment := range calculation.Installments {
		scheduled += installment.ScheduledAmount
	}
	if len(calculation.Installments) != 24 || scheduled != calculation.TotalObligation {
		t.Fatalf("unexpected installment schedule: count=%d sum=%d", len(calculation.Installments), scheduled)
	}
}

func TestCalculateRegularLoanScheduleBoundariesRoundingAndRemainder(t *testing.T) {
	for _, tc := range []struct {
		name, date                 string
		principal                  int64
		duration                   int
		monthly, total, obligation int64
	}{
		{"first tier boundary", "2026-01-01", 25_000_000, 1, 250_000, 250_000, 25_250_000},
		{"one month", "2026-01-01", 50, 1, 1, 1, 51},
		{"twenty four months", "2026-01-01", 25_000_001, 24, 250_000, 6_000_000, 31_000_001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := calculateRegularLoanSchedule(tc.principal, tc.duration, tc.date)
			if err != nil {
				t.Fatal(err)
			}
			if got.MonthlyAdminFee != tc.monthly || got.TotalAdminFee != tc.total || got.TotalObligation != tc.obligation {
				t.Fatalf("got monthly=%d total=%d obligation=%d", got.MonthlyAdminFee, got.TotalAdminFee, got.TotalObligation)
			}
		})
	}
	remainder, err := calculateRegularLoanSchedule(101, 2, "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if remainder.Installments[0].ScheduledAmount != 51 || remainder.Installments[1].ScheduledAmount != 52 {
		t.Fatalf("remainder schedule = %+v", remainder.Installments)
	}
}

func TestCalculateRegularLoanScheduleRejectsInvalidAndOverflowingInputs(t *testing.T) {
	for _, tc := range []struct {
		principal int64
		duration  int
	}{
		{0, 1}, {1, 0}, {1, 25}, {math.MaxInt64, 24},
	} {
		if _, err := calculateRegularLoanSchedule(tc.principal, tc.duration, "2026-01-01"); err == nil {
			t.Fatalf("expected error for principal=%d duration=%d", tc.principal, tc.duration)
		}
	}
}

func TestRegularLoanAdminFeeNumeratorOverflowBoundary(t *testing.T) {
	firstTierNumerator := regularLoanFirstTierLimit * 100
	maximumExcess := (math.MaxInt64 - firstTierNumerator) / 150
	maximumPrincipal := regularLoanFirstTierLimit + maximumExcess

	fee, err := calculateRegularLoanMonthlyAdminFeeV1(maximumPrincipal)
	if err != nil {
		t.Fatalf("exact safe numerator boundary rejected: %v", err)
	}
	expectedNumerator := firstTierNumerator + maximumExcess*150
	expectedFee := expectedNumerator / 10_000
	if expectedNumerator%10_000 >= 5_000 {
		expectedFee++
	}
	if fee != expectedFee {
		t.Fatalf("boundary fee=%d want=%d", fee, expectedFee)
	}
	if _, err := calculateRegularLoanMonthlyAdminFeeV1(maximumPrincipal + 1); err == nil {
		t.Fatal("principal one Rupiah past safe combined numerator boundary must fail")
	}
}

func TestLoanFeeSnapshotValidationAndScheduleUseStoredObligation(t *testing.T) {
	monthly := int64(325_000)
	if err := validateLoanFeeSnapshot(regularLoanAdminFeePolicy, 30_000_000, 24, &monthly, 7_800_000, 37_800_000); err != nil {
		t.Fatal(err)
	}
	for _, tampered := range []struct {
		monthly *int64
		total   int64
		owed    int64
	}{
		{nil, 7_800_000, 37_800_000},
		{&monthly, 7_800_001, 37_800_001},
		{&monthly, 7_800_000, 37_800_001},
	} {
		if err := validateLoanFeeSnapshot(regularLoanAdminFeePolicy, 30_000_000, 24, tampered.monthly, tampered.total, tampered.owed); err == nil {
			t.Fatal("expected tampered snapshot rejection")
		}
	}
	wrongMonthly := int64(1)
	if err := validateLoanFeeSnapshot(regularLoanAdminFeePolicy, 30_000_000, 24, &wrongMonthly, 24, 30_000_024); err == nil {
		t.Fatal("expected policy-version validation to reject internally consistent but incorrect fee terms")
	}
	legacy, err := buildLoanScheduleFromObligation(1_000_121, 121, "2026-01-31", int(^uint(0)>>1))
	if err != nil || len(legacy.Installments) != 121 || legacy.TotalObligation != 1_000_121 {
		t.Fatalf("legacy stored-obligation schedule = %+v, err=%v", legacy, err)
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
