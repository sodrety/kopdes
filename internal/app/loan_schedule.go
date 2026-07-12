package app

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	defaultLoanInterestRateBPS = 100
	maxLoanInterestRateBPS     = 1000
	maxLoanDurationMonths      = 120
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
	TotalObligation int64
	Installments    []InstallmentCalculation
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
