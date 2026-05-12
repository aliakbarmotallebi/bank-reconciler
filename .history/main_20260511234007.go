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
	DateCol        int
	DepositCol     int
	WithdrawCol    int
	SkipHeaderRows int
	InvertAmounts  bool
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
	if col >= 0 && col < len(row) {
		return row[col]
	}
	return ""
}

func detectColumns(headers []string) (dateCol, depositCol, withdrawCol int) {
	dateCol = -1
	depositCol = -1
	withdrawCol = -1

	for i, h := range headers {
		h = strings.TrimSpace(strings.ToLower(h))

		if strings.Contains(h, "date") || strings.Contains(h, "history") ||
			strings.Contains(h, "date") || h == "c" {
			dateCol = i
		}

		if strings.Contains(h, "deposit") || strings.Contains(h, "credit") ||
			strings.Contains(h, "variz") || strings.Contains(h, "bedehkar") ||
			strings.Contains(h, "e") {
			if depositCol == -1 {
				depositCol = i
			}
		}

		if strings.Contains(h, "withdraw") || strings.Contains(h, "debit") ||
			strings.Contains(h, "bardasht") || strings.Contains(h, "bestankar") ||
			strings.Contains(h, "f") {
			if withdrawCol == -1 {
				withdrawCol = i
			}
		}
	}

	return
}

func readTransactions(path string, cfg FileConfig, progress chan float64) (map[string]*DateTransactions, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read rows: %w", err)
	}

	data := make(map[string]*DateTransactions)
	totalRows := float64(len(rows))
	processedRows := 0

	for i, row := range rows {
		if i < cfg.SkipHeaderRows {
			continue
		}

		if progress != nil && i%50 == 0 {
			progress <- float64(i) / totalRows
		}

		date := strings.TrimSpace(getCell(row, cfg.DateCol))
		if date == "" || !strings.Contains(date, "/") || len(date) < 8 {
			continue
		}

		depositStr := getCell(row, cfg.DepositCol)
		withdrawStr := getCell(row, cfg.WithdrawCol)

		depositAmount := parseAmount(depositStr)
		withdrawAmount := parseAmount(withdrawStr)

		if cfg.InvertAmounts {
			depositAmount, withdrawAmount = withdrawAmount, depositAmount
		}

		if depositAmount == 0 && withdrawAmount == 0 {
			continue
		}

		if _, exists := data[date]; !exists {
			data[date] = &DateTransactions{}
		}

		if depositAmount > 0 {
			data[date].Deposits = append(data[date].Deposits, depositAmount)
		}

		if withdrawAmount > 0 {
			data[date].Withdraws = append(data[date].Withdraws, withdrawAmount)
		}

		processedRows++
	}

	if progress != nil {
		progress <- 1.0
	}

	fmt.Printf("File %s: processed %d rows, found %d unique dates\n",
		filepath.Base(path), processedRows, len(data))

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

	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
	})

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
		f.SetCellStyle(sheet, cell, cell, style)
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
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), "Discrepancy")
	}

	f.SetColWidth(sheet, "A", "H", 20)

	return f.SaveAs(outputPath)
}

func main() {
	myApp := app.New()
	myApp.Settings().SetTheme(theme.LightTheme())

	w := myApp.NewWindow("Bank Transaction Reconciler")
	w.Resize(fyne.NewSize(900, 650))

	var fileA string
	var fileB string
	var outputFile string

	fileALabel := widget.NewLabel("Not selected")
	fileBLabel := widget.NewLabel("Not selected")
	outputLabel := widget.NewLabel("Auto generated")

	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	statusLabel := widget.NewLabel("Ready")
	logText := widget.NewTextGrid()

	logWriter := func(msg string) {
		currentTime := time.Now().Format("15:04:05")
		statusLabel.SetText(msg)
		logText.SetText(logText.Text() + fmt.Sprintf("[%s] %s\n", currentTime, msg))
	}

	previewResults := widget.NewTextGrid()

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
		logWriter("Starting reconciliation...")

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

			var progress float64
			go func() {
				for {
					select {
					case p := <-progressA:
						progress = p * 0.4
						progressBar.SetValue(progress)
					case p := <-progressB:
						progress = 0.4 + p*0.4
						progressBar.SetValue(progress)
					}
				}
			}()

			cfgA := FileConfig{
				DateCol:        1,
				DepositCol:     3,
				WithdrawCol:    2,
				SkipHeaderRows: 1,
				InvertAmounts:  false,
			}

			logWriter("Reading File A...")
			dataA, err := readTransactions(fileA, cfgA, progressA)
			if err != nil {
				dialog.ShowError(err, w)
				runButton.Enable()
				progressBar.Hide()
				return
			}
			logWriter(fmt.Sprintf("File A: found %d dates with transactions", len(dataA)))

			cfgB := FileConfig{
				DateCol:        2,
				DepositCol:     4,
				WithdrawCol:    5,
				SkipHeaderRows: 3,
				InvertAmounts:  false,
			}

			logWriter("Reading File B...")
			dataB, err := readTransactions(fileB, cfgB, progressB)
			if err != nil {
				dialog.ShowError(err, w)
				runButton.Enable()
				progressBar.Hide()
				return
			}
			logWriter(fmt.Sprintf("File B: found %d dates with transactions", len(dataB)))

			progressBar.SetValue(0.85)
			logWriter("Comparing transactions...")

			results := reconcile(dataA, dataB)

			logWriter(fmt.Sprintf("Found %d dates with discrepancies", len(results)))

			progressBar.SetValue(0.95)
			logWriter("Generating report...")

			err = generateReport(results, outputFile)
			if err != nil {
				dialog.ShowError(err, w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			progressBar.SetValue(1.0)
			logWriter(fmt.Sprintf("Report saved: %s", filepath.Base(outputFile)))

			previewText := fmt.Sprintf("Total discrepancy dates: %d\n\n", len(results))
			shown := 0
			for _, r := range results {
				if shown >= 20 {
					previewText += fmt.Sprintf("\n... and %d more dates", len(results)-20)
					break
				}
				previewText += fmt.Sprintf("Date: %s\n", r.Date)
				if len(r.MissingInB) > 0 {
					previewText += fmt.Sprintf("  Missing in B: %d txns\n", len(r.MissingInB))
				}
				if len(r.MissingInA) > 0 {
					previewText += fmt.Sprintf("  Missing in A: %d txns\n", len(r.MissingInA))
				}
				shown++
			}
			previewResults.SetText(previewText)

			time.Sleep(500 * time.Millisecond)
			progressBar.Hide()
			runButton.Enable()

			dialog.ShowInformation("Complete",
				fmt.Sprintf("Reconciliation completed!\n%d discrepancies found.\n\nReport: %s",
					len(results), filepath.Base(outputFile)), w)
		}()
	}

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

	configInfo := widget.NewCard("Column Configuration", "",
		widget.NewLabel("File A: Col B=Date, Col C=Withdraw, Col D=Deposit\nFile B: Col C=Date, Col E=Debit, Col F=Credit"),
	)

	leftPanel := container.NewVBox(
		fileSelection,
		configInfo,
		runButton,
		progressBar,
		statusLabel,
	)

	rightPanel := container.NewVSplit(
		container.NewVBox(
			widget.NewLabel("Results Preview:"),
			previewResults,
		),
		container.NewVBox(
			widget.NewLabel("Process Log:"),
			logText,
		),
	)
	rightPanel.SetOffset(0.5)

	content := container.NewHSplit(leftPanel, rightPanel)
	content.SetOffset(0.45)

	w.SetContent(content)
	w.ShowAndRun()
}
