package app

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	defaultLoanInterestRateBPS   = 100
	maxLoanInterestRateBPS       = 1000
	maxLoanDurationMonths        = 120
	maxRegularLoanDurationMonths = 24
	maxSecondaryGoodsDuration    = 12
	regularLoanAdminFeePolicy    = "regular_tiered_monthly_v1"
	secondaryGoodsAdminFeePolicy = "secondary_goods_one_time_v1"
	paylaterAdminFeePolicy       = "goods_purchase_paylater_one_time_v1"
	regularLoanFirstTierLimit    = int64(25_000_000)
)

var jakartaLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("Asia/Jakarta", 7*60*60)
	}
	return location
}()

type InstallmentCalculation struct {
	Number          int
	DueDate         string
	ScheduledAmount int64
	PaidAmount      int64
}

type LoanScheduleCalculation struct {
	TotalInterest   int64
	MonthlyAdminFee int64
	TotalAdminFee   int64
	TotalObligation int64
	Installments    []InstallmentCalculation
}

// buildLoanScheduleFromObligation rebuilds only installment amounts and dates.
// It deliberately knows nothing about fee rates so an approved financial
// snapshot remains stable when policy code changes later.
func buildLoanScheduleFromObligation(totalObligation int64, durationMonths int, startDate string, maxDuration int) (LoanScheduleCalculation, error) {
	if totalObligation <= 0 {
		return LoanScheduleCalculation{}, errors.New("total obligation must be positive")
	}
	if durationMonths <= 0 || durationMonths > maxDuration {
		return LoanScheduleCalculation{}, fmt.Errorf("duration must be between 1 and %d months", maxDuration)
	}
	if totalObligation < int64(durationMonths) {
		return LoanScheduleCalculation{}, errors.New("total obligation must allow at least one Rupiah per installment")
	}
	start, err := parseLoanDate(startDate)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}

	regularAmount := totalObligation / int64(durationMonths)
	remainder := totalObligation % int64(durationMonths)
	installments := make([]InstallmentCalculation, durationMonths)
	for index := range installments {
		amount := regularAmount
		if index == len(installments)-1 {
			amount += remainder
		}
		installments[index] = InstallmentCalculation{
			Number:          index + 1,
			DueDate:         loanDueDate(start, index+1).Format("2006-01-02"),
			ScheduledAmount: amount,
		}
	}
	return LoanScheduleCalculation{TotalObligation: totalObligation, Installments: installments}, nil
}

func validateLoanFeeSnapshot(policy string, principal int64, durationMonths int, monthlyAdminFee *int64, totalAdminFee, totalObligation int64) error {
	if principal <= 0 || durationMonths <= 0 || totalAdminFee < 0 || totalObligation <= 0 || principal > math.MaxInt64-totalAdminFee || principal+totalAdminFee != totalObligation {
		return errors.New("invalid loan fee snapshot totals")
	}
	switch policy {
	case regularLoanAdminFeePolicy:
		expectedMonthly, err := calculateRegularLoanMonthlyAdminFeeV1(principal)
		if err != nil || monthlyAdminFee == nil || *monthlyAdminFee != expectedMonthly || *monthlyAdminFee > math.MaxInt64/int64(durationMonths) || *monthlyAdminFee*int64(durationMonths) != totalAdminFee || durationMonths > maxRegularLoanDurationMonths {
			return errors.New("invalid Regular Loan fee snapshot")
		}
	case secondaryGoodsAdminFeePolicy:
		expectedFee, err := calculatePercentAdminFee(principal, 20)
		if err != nil || monthlyAdminFee != nil || durationMonths > maxSecondaryGoodsDuration || totalAdminFee != expectedFee {
			return errors.New("invalid Secondary Goods Loan fee snapshot")
		}
	case paylaterAdminFeePolicy:
		expectedFee, err := calculatePercentAdminFee(principal, 5)
		if err != nil || monthlyAdminFee != nil || durationMonths != 1 || totalAdminFee != expectedFee {
			return errors.New("invalid Paylater Loan fee snapshot")
		}
	case "legacy_flat_monthly":
		// Historical monthly rates were not snapshotted reliably. The preserved
		// total is authoritative; no derived monthly value may be invented.
		if monthlyAdminFee != nil {
			return errors.New("legacy monthly admin fee must be absent")
		}
	default:
		return fmt.Errorf("unknown admin fee policy %q", policy)
	}
	return nil
}

func calculatePercentAdminFee(principal int64, percent int64) (int64, error) {
	if principal <= 0 {
		return 0, errors.New("principal must be positive")
	}
	if percent <= 0 || principal > (math.MaxInt64-50)/percent {
		return 0, errors.New("loan admin fee calculation overflow")
	}
	return (principal*percent + 50) / 100, nil
}

func calculateRegularLoanMonthlyAdminFeeV1(principal int64) (int64, error) {
	if principal <= 0 {
		return 0, errors.New("principal must be positive")
	}
	firstTier := principal
	if firstTier > regularLoanFirstTierLimit {
		firstTier = regularLoanFirstTierLimit
	}
	excess := principal - firstTier
	firstTierNumerator := firstTier * 100
	if excess > (math.MaxInt64-firstTierNumerator)/150 {
		return 0, errors.New("regular loan admin fee calculation overflow")
	}
	feeNumerator := firstTierNumerator + excess*150
	monthlyAdminFee := feeNumerator / 10_000
	if feeNumerator%10_000 >= 5_000 {
		monthlyAdminFee++
	}
	return monthlyAdminFee, nil
}

func calculateSecondaryGoodsLoanSchedule(principal int64, durationMonths int, startDate string) (LoanScheduleCalculation, error) {
	if durationMonths <= 0 || durationMonths > maxSecondaryGoodsDuration {
		return LoanScheduleCalculation{}, fmt.Errorf("secondary goods loan duration must be between 1 and %d months", maxSecondaryGoodsDuration)
	}
	totalAdminFee, err := calculatePercentAdminFee(principal, 20)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}
	if principal > math.MaxInt64-totalAdminFee {
		return LoanScheduleCalculation{}, errors.New("secondary goods loan obligation calculation overflow")
	}
	calc, err := buildLoanScheduleFromObligation(principal+totalAdminFee, durationMonths, startDate, maxSecondaryGoodsDuration)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}
	calc.TotalAdminFee = totalAdminFee
	return calc, nil
}

func calculatePaylaterLoanSchedule(principal int64, durationMonths int, startDate string) (LoanScheduleCalculation, error) {
	if durationMonths != 1 {
		return LoanScheduleCalculation{}, errors.New("paylater loan duration must be 1 month")
	}
	totalAdminFee, err := calculatePercentAdminFee(principal, 5)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}
	if principal > math.MaxInt64-totalAdminFee {
		return LoanScheduleCalculation{}, errors.New("paylater loan obligation calculation overflow")
	}
	calc, err := buildLoanScheduleFromObligation(principal+totalAdminFee, 1, startDate, 1)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}
	calc.TotalAdminFee = totalAdminFee
	return calc, nil
}

func calculateRegularLoanSchedule(principal int64, durationMonths int, startDate string) (LoanScheduleCalculation, error) {
	if principal <= 0 {
		return LoanScheduleCalculation{}, errors.New("principal must be positive")
	}
	if durationMonths <= 0 || durationMonths > maxRegularLoanDurationMonths {
		return LoanScheduleCalculation{}, fmt.Errorf("regular loan duration must be between 1 and %d months", maxRegularLoanDurationMonths)
	}
	start, err := parseLoanDate(startDate)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}

	monthlyAdminFee, err := calculateRegularLoanMonthlyAdminFeeV1(principal)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}
	if monthlyAdminFee > math.MaxInt64/int64(durationMonths) {
		return LoanScheduleCalculation{}, errors.New("regular loan total admin fee calculation overflow")
	}
	totalAdminFee := monthlyAdminFee * int64(durationMonths)
	if principal > math.MaxInt64-totalAdminFee {
		return LoanScheduleCalculation{}, errors.New("regular loan obligation calculation overflow")
	}
	totalObligation := principal + totalAdminFee
	if totalObligation < int64(durationMonths) {
		return LoanScheduleCalculation{}, errors.New("total obligation must allow at least one Rupiah per installment")
	}

	regularAmount := totalObligation / int64(durationMonths)
	remainder := totalObligation % int64(durationMonths)
	installments := make([]InstallmentCalculation, durationMonths)
	for index := range installments {
		amount := regularAmount
		if index == len(installments)-1 {
			amount += remainder
		}
		installments[index] = InstallmentCalculation{
			Number:          index + 1,
			DueDate:         loanDueDate(start, index+1).Format("2006-01-02"),
			ScheduledAmount: amount,
		}
	}
	return LoanScheduleCalculation{
		MonthlyAdminFee: monthlyAdminFee,
		TotalAdminFee:   totalAdminFee,
		TotalObligation: totalObligation,
		Installments:    installments,
	}, nil
}

func parseLoanDate(value string) (time.Time, error) {
	date, err := time.ParseInLocation("2006-01-02", value, jakartaLocation)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("invalid loan date %q: expected YYYY-MM-DD", value)
	}
	return date, nil
}

func loanDueDate(start time.Time, installmentNumber int) time.Time {
	firstOfTargetMonth := time.Date(start.Year(), start.Month()+time.Month(installmentNumber), 1, 0, 0, 0, 0, jakartaLocation)
	lastDay := time.Date(firstOfTargetMonth.Year(), firstOfTargetMonth.Month()+1, 0, 0, 0, 0, 0, jakartaLocation).Day()
	day := start.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(firstOfTargetMonth.Year(), firstOfTargetMonth.Month(), day, 0, 0, 0, 0, jakartaLocation)
}

func calculateLoanSchedule(principal int64, durationMonths, interestRateBPS int, startDate string) (LoanScheduleCalculation, error) {
	return calculateLoanScheduleWithMaxDuration(principal, durationMonths, interestRateBPS, startDate, maxLoanDurationMonths)
}

// calculateLegacyLoanSchedule is migration-only. Legacy data predates the
// 120-month write limit, so migration must preserve its original tenor while
// all current request and approval paths continue through calculateLoanSchedule.
func calculateLegacyLoanSchedule(principal int64, durationMonths, interestRateBPS int, startDate string) (LoanScheduleCalculation, error) {
	return calculateLoanScheduleWithMaxDuration(principal, durationMonths, interestRateBPS, startDate, int(^uint(0)>>1))
}

func calculateLoanScheduleWithMaxDuration(principal int64, durationMonths, interestRateBPS int, startDate string, maxDuration int) (LoanScheduleCalculation, error) {
	if principal <= 0 {
		return LoanScheduleCalculation{}, errors.New("principal must be positive")
	}
	if durationMonths <= 0 || durationMonths > maxDuration {
		return LoanScheduleCalculation{}, fmt.Errorf("duration must be between 1 and %d months", maxDuration)
	}
	if interestRateBPS < 0 || interestRateBPS > maxLoanInterestRateBPS {
		return LoanScheduleCalculation{}, fmt.Errorf("interest rate must be between 0 and %d basis points", maxLoanInterestRateBPS)
	}
	start, err := parseLoanDate(startDate)
	if err != nil {
		return LoanScheduleCalculation{}, err
	}

	// Flat interest is rounded to the nearest whole Rupiah, half up.
	multiplier := int64(interestRateBPS) * int64(durationMonths)
	if multiplier != 0 && principal > math.MaxInt64/multiplier {
		return LoanScheduleCalculation{}, errors.New("loan interest calculation overflow")
	}
	numerator := principal * multiplier
	totalInterest := numerator / 10000
	if numerator%10000 >= 5000 {
		totalInterest++
	}
	if totalInterest > 0 && principal > math.MaxInt64-totalInterest {
		return LoanScheduleCalculation{}, errors.New("loan obligation calculation overflow")
	}
	totalObligation := principal + totalInterest
	if totalObligation < int64(durationMonths) {
		return LoanScheduleCalculation{}, errors.New("total obligation must allow at least one Rupiah per installment")
	}
	regularAmount := totalObligation / int64(durationMonths)
	remainder := totalObligation % int64(durationMonths)

	installments := make([]InstallmentCalculation, durationMonths)
	for index := range installments {
		amount := regularAmount
		if index == len(installments)-1 {
			amount += remainder
		}
		installments[index] = InstallmentCalculation{
			Number:          index + 1,
			DueDate:         loanDueDate(start, index+1).Format("2006-01-02"),
			ScheduledAmount: amount,
		}
	}
	return LoanScheduleCalculation{TotalInterest: totalInterest, TotalObligation: totalObligation, Installments: installments}, nil
}

func allocateRepaymentsOldestFirst(installments []InstallmentCalculation, repayments []int64) (int64, error) {
	allocated := append([]InstallmentCalculation(nil), installments...)
	var totalPaid int64
	for _, repayment := range repayments {
		if repayment <= 0 {
			return 0, errors.New("repayment must be positive")
		}
		remaining := repayment
		for index := range allocated {
			unpaid := allocated[index].ScheduledAmount - allocated[index].PaidAmount
			if unpaid <= 0 {
				continue
			}
			applied := remaining
			if applied > unpaid {
				applied = unpaid
			}
			allocated[index].PaidAmount += applied
			remaining -= applied
			totalPaid += applied
			if remaining == 0 {
				break
			}
		}
		if remaining > 0 {
			return totalPaid, errors.New("repayments exceed total obligation")
		}
	}
	copy(installments, allocated)
	return totalPaid, nil
}
