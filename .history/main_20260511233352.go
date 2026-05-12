package main

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/xuri/excelize/v2"
)

type TransactionType int

const (
	Deposit  TransactionType = 1
	Withdraw TransactionType = 2
)

type Transaction struct {
	Amount float64
	Type   TransactionType
}

type DateTransactions struct {
	Deposits  []float64
	Withdraws []float64
}

type ReconcileResult struct {
	Date           string
	DepositsFileA  []float64
	WithdrawsFileA []float64
	DepositsFileB  []float64
	WithdrawsFileB []float64
	MissingInB     []float64
	MissingInA     []float64
}

type FileConfig struct {
	DateCol     int
	DepositCol  int
	WithdrawCol int
}

type ColumnMapping struct {
	Date     int
	Deposit  int
	Withdraw int
}

func parseAmount(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || s == "0" {
		return 0
	}
	amount, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return amount
}

func getCell(row []string, col int) string {
	if col < len(row) {
		return row[col]
	}
	return ""
}

func readTransactions(path string, cfg FileConfig, progress chan float64) (map[string]*DateTransactions, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		return nil, fmt.Errorf("failed to read rows: %w", err)
	}

	data := make(map[string]*DateTransactions)
	totalRows := float64(len(rows))

	for i, row := range rows {
		if i == 0 {
			continue
		}

		if progress != nil && i%10 == 0 {
			progress <- float64(i) / totalRows
		}

		date := strings.TrimSpace(getCell(row, cfg.DateCol))
		if date == "" {
			continue
		}

		if !strings.Contains(date, "/") || len(date) < 8 {
			continue
		}

		depositAmount := parseAmount(getCell(row, cfg.DepositCol))
		withdrawAmount := parseAmount(getCell(row, cfg.WithdrawCol))

		if _, exists := data[date]; !exists {
			data[date] = &DateTransactions{}
		}

		if depositAmount > 0 {
			data[date].Deposits = append(data[date].Deposits, depositAmount)
		}

		if withdrawAmount > 0 {
			data[date].Withdraws = append(data[date].Withdraws, withdrawAmount)
		}
	}

	if progress != nil {
		progress <- 1.0
	}

	return data, nil
}

func matchTransactions(listA, listB []float64, missingInB, missingInA *[]float64) {
	remainingA := make([]float64, len(listA))
	remainingB := make([]float64, len(listB))
	copy(remainingA, listA)
	copy(remainingB, listB)

	sort.Float64s(remainingA)
	sort.Float64s(remainingB)

	i, j := 0, 0
	for i < len(remainingA) && j < len(remainingB) {
		diff := remainingA[i] - remainingB[j]

		if math.Abs(diff) < 0.01 {
			i++
			j++
		} else if diff < 0 {
			*missingInB = append(*missingInB, remainingA[i])
			i++
		} else {
			*missingInA = append(*missingInA, remainingB[j])
			j++
		}
	}

	for ; i < len(remainingA); i++ {
		*missingInB = append(*missingInB, remainingA[i])
	}
	for ; j < len(remainingB); j++ {
		*missingInA = append(*missingInA, remainingB[j])
	}
}

func reconcile(dataA, dataB map[string]*DateTransactions) []ReconcileResult {
	allDates := make(map[string]bool)
	for date := range dataA {
		allDates[date] = true
	}
	for date := range dataB {
		allDates[date] = true
	}

	uniqueDates := make([]string, 0, len(allDates))
	for date := range allDates {
		uniqueDates = append(uniqueDates, date)
	}
	sort.Strings(uniqueDates)

	var results []ReconcileResult

	for _, date := range uniqueDates {
		transA := dataA[date]
		transB := dataB[date]

		if transA == nil {
			transA = &DateTransactions{}
		}
		if transB == nil {
			transB = &DateTransactions{}
		}

		result := ReconcileResult{
			Date:           date,
			DepositsFileA:  transA.Deposits,
			WithdrawsFileA: transA.Withdraws,
			DepositsFileB:  transB.Deposits,
			WithdrawsFileB: transB.Withdraws,
		}

		matchTransactions(transA.Deposits, transB.Deposits, &result.MissingInB, &result.MissingInA)
		matchTransactions(transA.Withdraws, transB.Withdraws, &result.MissingInB, &result.MissingInA)

		if len(result.MissingInA) > 0 || len(result.MissingInB) > 0 {
			results = append(results, result)
		}
	}

	return results
}

func formatAmounts(amounts []float64) string {
	if len(amounts) == 0 {
		return "0"
	}

	parts := make([]string, len(amounts))
	for i, a := range amounts {
		parts[i] = fmt.Sprintf("%.0f", a)
	}
	return strings.Join(parts, " | ")
}

func generateReport(results []ReconcileResult, outputPath string) error {
	f := excelize.NewFile()
	sheet := "Reconciliation Report"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{
		"Date",
		"Deposits File A",
		"Withdraws File A",
		"Deposits File B",
		"Withdraws File B",
		"Missing in File B",
		"Missing in File A",
		"Status",
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	for i, r := range results {
		row := i + 2
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), r.Date)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), formatAmounts(r.DepositsFileA))
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), formatAmounts(r.WithdrawsFileA))
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), formatAmounts(r.DepositsFileB))
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), formatAmounts(r.WithdrawsFileB))
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), formatAmounts(r.MissingInB))
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), formatAmounts(r.MissingInA))

		status := "Discrepancy"
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), status)
	}

	return f.SaveAs(outputPath)
}

func main() {
	myApp := app.New()
	myApp.Settings().SetTheme(theme.LightTheme())

	w := myApp.NewWindow("Bank Transaction Reconciler")
	w.Resize(fyne.NewSize(800, 600))

	var fileA string
	var fileB string
	var outputFile string

	mappingA := &ColumnMapping{Date: 1, Deposit: 3, Withdraw: 2}
	mappingB := &ColumnMapping{Date: 2, Deposit: 4, Withdraw: 5}

	fileALabel := widget.NewLabel("Not selected")
	fileBLabel := widget.NewLabel("Not selected")
	outputLabel := widget.NewLabel("Auto generated")

	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	statusLabel := widget.NewLabel("")
	logText := widget.NewTextGrid()

	logWriter := func(msg string) {
		currentTime := time.Now().Format("15:04:05")
		statusLabel.SetText(msg)
		logText.SetText(logText.Text() + fmt.Sprintf("[%s] %s\n", currentTime, msg))
	}

	previewResults := widget.NewTextGrid()
	previewLabel := widget.NewLabel("Results Preview:")

	selectFileA := widget.NewButton("Select File A", func() {
		dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if r == nil {
				return
			}
			fileA = r.URI().Path()
			fileALabel.SetText(filepath.Base(fileA))
			logWriter(fmt.Sprintf("File A selected: %s", filepath.Base(fileA)))
		}, w).Show()
	})

	selectFileB := widget.NewButton("Select File B", func() {
		dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if r == nil {
				return
			}
			fileB = r.URI().Path()
			fileBLabel.SetText(filepath.Base(fileB))
			logWriter(fmt.Sprintf("File B selected: %s", filepath.Base(fileB)))
		}, w).Show()
	})

	selectOutput := widget.NewButton("Select Output", func() {
		dialog.NewFileSave(func(r fyne.URIWriteCloser, err error) {
			if r == nil {
				return
			}
			outputFile = r.URI().Path()
			r.Close()
			if filepath.Ext(outputFile) == "" {
				outputFile += ".xlsx"
			}
			outputLabel.SetText(filepath.Base(outputFile))
			logWriter(fmt.Sprintf("Output file set: %s", filepath.Base(outputFile)))
		}, w).Show()
	})

	runButton := widget.NewButton("Start Reconciliation", nil)
	runButton.Importance = widget.HighImportance

	runButton.OnTapped = func() {
		if fileA == "" || fileB == "" {
			dialog.ShowError(fmt.Errorf("please select both files"), w)
			return
		}

		runButton.Disable()
		progressBar.Show()
		progressBar.SetValue(0)
		logWriter("Starting reconciliation process...")

		if outputFile == "" {
			outputFile = filepath.Join(
				filepath.Dir(fileA),
				fmt.Sprintf("reconciliation_report_%s.xlsx", time.Now().Format("20060102_150405")),
			)
			outputLabel.SetText(filepath.Base(outputFile))
		}

		go func() {
			progressA := make(chan float64, 100)
			progressB := make(chan float64, 100)

			logWriter("Reading File A...")

			cfgA := FileConfig{
				DateCol:     mappingA.Date,
				DepositCol:  mappingA.Deposit,
				WithdrawCol: mappingA.Withdraw,
			}

			dataA, err := readTransactions(fileA, cfgA, progressA)
			if err != nil {
				dialog.ShowError(err, w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			logWriter("Reading File B...")

			cfgB := FileConfig{
				DateCol:     mappingB.Date,
				DepositCol:  mappingB.Deposit,
				WithdrawCol: mappingB.Withdraw,
			}

			dataB, err := readTransactions(fileB, cfgB, progressB)
			if err != nil {
				dialog.ShowError(err, w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			progressBar.SetValue(0.8)
			logWriter("Comparing transactions...")

			results := reconcile(dataA, dataB)

			logWriter(fmt.Sprintf("Found %d dates with discrepancies", len(results)))

			progressBar.SetValue(0.9)
			logWriter("Generating report...")

			err = generateReport(results, outputFile)
			if err != nil {
				dialog.ShowError(err, w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			progressBar.SetValue(1.0)
			logWriter(fmt.Sprintf("Report saved to: %s", filepath.Base(outputFile)))

			previewText := fmt.Sprintf("Total dates with discrepancies: %d\n\n", len(results))
			for i, r := range results {
				if i >= 10 {
					previewText += fmt.Sprintf("\n... and %d more dates", len(results)-10)
					break
				}
				previewText += fmt.Sprintf("Date: %s\n", r.Date)
				if len(r.MissingInB) > 0 {
					previewText += fmt.Sprintf("  Missing in B: %d transactions\n", len(r.MissingInB))
				}
				if len(r.MissingInA) > 0 {
					previewText += fmt.Sprintf("  Missing in A: %d transactions\n", len(r.MissingInA))
				}
			}
			previewResults.SetText(previewText)

			time.Sleep(500 * time.Millisecond)
			progressBar.Hide()
			runButton.Enable()

			dialog.ShowInformation("Complete",
				fmt.Sprintf("Reconciliation completed!\n%d discrepancies found.\nReport saved to:\n%s",
					len(results), outputFile), w)
		}()
	}

	mappingSection := widget.NewCard("Column Mapping Configuration", "",
		container.NewVBox(
			widget.NewLabel("File A (ExtendedInvoice):"),
			container.NewGridWithColumns(3,
				widget.NewLabel("Date Column"),
				widget.NewLabel("Deposit Column"),
				widget.NewLabel("Withdraw Column"),
			),
			container.NewGridWithColumns(3,
				widget.NewLabel(fmt.Sprintf("Index: %d", mappingA.Date)),
				widget.NewLabel(fmt.Sprintf("Index: %d", mappingA.Deposit)),
				widget.NewLabel(fmt.Sprintf("Index: %d", mappingA.Withdraw)),
			),
			widget.NewSeparator(),
			widget.NewLabel("File B (AMISA):"),
			container.NewGridWithColumns(3,
				widget.NewLabel("Date Column"),
				widget.NewLabel("Deposit Column"),
				widget.NewLabel("Withdraw Column"),
			),
			container.NewGridWithColumns(3,
				widget.NewLabel(fmt.Sprintf("Index: %d", mappingB.Date)),
				widget.NewLabel(fmt.Sprintf("Index: %d", mappingB.Deposit)),
				widget.NewLabel(fmt.Sprintf("Index: %d", mappingB.Withdraw)),
			),
		),
	)

	fileSelection := widget.NewCard("File Selection", "",
		container.NewVBox(
			container.NewGridWithColumns(2,
				widget.NewLabel("File A:"),
				fileALabel,
			),
			selectFileA,
			container.NewGridWithColumns(2,
				widget.NewLabel("File B:"),
				fileBLabel,
			),
			selectFileB,
			container.NewGridWithColumns(2,
				widget.NewLabel("Output:"),
				outputLabel,
			),
			selectOutput,
		),
	)

	leftPanel := container.NewVBox(
		fileSelection,
		mappingSection,
		runButton,
		progressBar,
		statusLabel,
	)

	rightPanel := container.NewVSplit(
		container.NewVBox(
			previewLabel,
			previewResults,
		),
		container.NewVBox(
			widget.NewLabel("Process Log:"),
			logText,
		),
	)
	rightPanel.SetOffset(0.4)

	content := container.NewHSplit(leftPanel, rightPanel)
	content.SetOffset(0.45)

	w.SetContent(content)
	w.ShowAndRun()
}
