package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
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

type Transaction struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
	Type   string  `json:"type"`
}

type FileData struct {
	FileName     string        `json:"file_name"`
	TotalRows    int           `json:"total_rows"`
	Transactions []Transaction `json:"transactions"`
}

type DateGroup struct {
	Deposits  []float64
	Withdraws []float64
}

type ReconcileResult struct {
	Date           string    `json:"date"`
	DepositsFileA  []float64 `json:"deposits_file_a"`
	WithdrawsFileA []float64 `json:"withdraws_file_a"`
	DepositsFileB  []float64 `json:"deposits_file_b"`
	WithdrawsFileB []float64 `json:"withdraws_file_b"`
	MissingInB     []float64 `json:"missing_in_b"`
	MissingInA     []float64 `json:"missing_in_a"`
}

type ColumnMapping struct {
	DateCol     int
	DepositCol  int
	WithdrawCol int
	SkipRows    int
}

func parseAmount(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || s == "0" || s == "-" {
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
		return strings.TrimSpace(row[col])
	}
	return ""
}

func detectColumnMapping(rows [][]string) ColumnMapping {
	mapping := ColumnMapping{
		DateCol:     -1,
		DepositCol:  -1,
		WithdrawCol: -1,
		SkipRows:    0,
	}

	for rowIdx, row := range rows {
		if len(row) == 0 {
			mapping.SkipRows++
			continue
		}

		nonEmptyCount := 0
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				nonEmptyCount++
			}
		}

		if nonEmptyCount < 2 {
			mapping.SkipRows++
			continue
		}

		headerKeywords := map[string]int{
			"تاریخ":    -1,
			"تاریخ":    -1,
			"برداشت":   -1,
			"واریز":    -1,
			"بدهکار":   -1,
			"بستانکار": -1,
			"شرح":      -1,
		}

		isHeader := false
		for colIdx, cell := range row {
			cell = strings.TrimSpace(cell)
			for keyword := range headerKeywords {
				if strings.Contains(cell, keyword) {
					headerKeywords[keyword] = colIdx
					isHeader = true
				}
			}
		}

		if isHeader {
			if headerKeywords["تاریخ"] >= 0 {
				mapping.DateCol = headerKeywords["تاریخ"]
			} else if headerKeywords["تاریخ"] >= 0 {
				mapping.DateCol = headerKeywords["تاریخ"]
			}

			if headerKeywords["واریز"] >= 0 {
				mapping.DepositCol = headerKeywords["واریز"]
			} else if headerKeywords["بدهکار"] >= 0 {
				mapping.DepositCol = headerKeywords["بدهکار"]
			}

			if headerKeywords["برداشت"] >= 0 {
				mapping.WithdrawCol = headerKeywords["برداشت"]
			} else if headerKeywords["بستانکار"] >= 0 {
				mapping.WithdrawCol = headerKeywords["بستانکار"]
			}

			if headerKeywords["شرح"] >= 0 && mapping.DateCol == -1 {
				mapping.DateCol = headerKeywords["شرح"] + 1
			}

			mapping.SkipRows = rowIdx + 1
			break
		}

		hasDate := false
		dateColCandidate := -1
		numberCols := []int{}

		for colIdx, cell := range row {
			cell = strings.TrimSpace(cell)
			if cell == "" || cell == "-" {
				continue
			}

			if strings.Contains(cell, "/") && len(cell) >= 8 && len(cell) <= 10 {
				hasDate = true
				if dateColCandidate == -1 {
					dateColCandidate = colIdx
				}
				continue
			}

			if _, err := strconv.ParseFloat(strings.ReplaceAll(cell, ",", ""), 64); err == nil {
				numberCols = append(numberCols, colIdx)
			}
		}

		if hasDate && len(numberCols) >= 1 {
			mapping.DateCol = dateColCandidate
			mapping.SkipRows = rowIdx

			if len(numberCols) == 2 {
				mapping.WithdrawCol = numberCols[0]
				mapping.DepositCol = numberCols[1]
			} else if len(numberCols) == 1 {
				mapping.WithdrawCol = numberCols[0]
				mapping.DepositCol = numberCols[0]
			}

			break
		}

		mapping.SkipRows++
	}

	if mapping.DepositCol == -1 && mapping.WithdrawCol >= 0 {
		mapping.DepositCol = mapping.WithdrawCol
	}

	return mapping
}

func excelToJSON(filePath string) (*FileData, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open excel file: %w", err)
	}
	defer f.Close()

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		return nil, fmt.Errorf("cannot read rows: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("empty file")
	}

	mapping := detectColumnMapping(rows)

	if mapping.DateCol == -1 {
		mapping.DateCol = 1
		mapping.WithdrawCol = 2
		mapping.DepositCol = 3
		mapping.SkipRows = 1
	}

	fmt.Printf("Detected mapping for %s:\n", filepath.Base(filePath))
	fmt.Printf("  Date column: %d\n", mapping.DateCol)
	fmt.Printf("  Deposit column: %d\n", mapping.DepositCol)
	fmt.Printf("  Withdraw column: %d\n", mapping.WithdrawCol)
	fmt.Printf("  Skip rows: %d\n", mapping.SkipRows)

	data := &FileData{
		FileName:  filepath.Base(filePath),
		TotalRows: 0,
	}

	for i, row := range rows {
		if i < mapping.SkipRows {
			continue
		}

		date := getCell(row, mapping.DateCol)

		if date == "" || !strings.Contains(date, "/") || len(date) < 8 {
			continue
		}

		if date == getCell(row, 0) {
			if _, err := strconv.Atoi(date); err == nil {
				continue
			}
		}

		withdrawStr := getCell(row, mapping.WithdrawCol)
		depositStr := getCell(row, mapping.DepositCol)

		withdrawAmount := parseAmount(withdrawStr)
		depositAmount := parseAmount(depositStr)

		if withdrawAmount == 0 && depositAmount == 0 {
			continue
		}

		if withdrawAmount > 0 && mapping.WithdrawCol != mapping.DepositCol {
			data.Transactions = append(data.Transactions, Transaction{
				Date:   date,
				Amount: withdrawAmount,
				Type:   "withdraw",
			})
		}

		if depositAmount > 0 {
			data.Transactions = append(data.Transactions, Transaction{
				Date:   date,
				Amount: depositAmount,
				Type:   "deposit",
			})
		}

		if withdrawAmount > 0 && mapping.WithdrawCol == mapping.DepositCol {
			data.Transactions = append(data.Transactions, Transaction{
				Date:   date,
				Amount: withdrawAmount,
				Type:   "withdraw",
			})
		}
	}

	data.TotalRows = len(data.Transactions)

	return data, nil
}

func saveJSON(data *FileData, filePath string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, jsonData, 0644)
}

func groupByDate(transactions []Transaction) map[string]*DateGroup {
	groups := make(map[string]*DateGroup)

	for _, t := range transactions {
		if _, exists := groups[t.Date]; !exists {
			groups[t.Date] = &DateGroup{}
		}

		if t.Type == "deposit" {
			groups[t.Date].Deposits = append(groups[t.Date].Deposits, t.Amount)
		} else {
			groups[t.Date].Withdraws = append(groups[t.Date].Withdraws, t.Amount)
		}
	}

	return groups
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

func reconcile(dataA, dataB *FileData) []ReconcileResult {
	groupA := groupByDate(dataA.Transactions)
	groupB := groupByDate(dataB.Transactions)

	allDates := make(map[string]bool)
	for date := range groupA {
		allDates[date] = true
	}
	for date := range groupB {
		allDates[date] = true
	}

	uniqueDates := make([]string, 0, len(allDates))
	for date := range allDates {
		uniqueDates = append(uniqueDates, date)
	}
	sort.Strings(uniqueDates)

	var results []ReconcileResult

	for _, date := range uniqueDates {
		transA := groupA[date]
		transB := groupB[date]

		if transA == nil {
			transA = &DateGroup{}
		}
		if transB == nil {
			transB = &DateGroup{}
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

func generateJSONReport(results []ReconcileResult, outputPath string) error {
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, jsonData, 0644)
}

func generateExcelReport(results []ReconcileResult, outputPath string) error {
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

	formatAmounts := func(amounts []float64) string {
		if len(amounts) == 0 {
			return "0"
		}
		parts := make([]string, len(amounts))
		for i, a := range amounts {
			parts[i] = fmt.Sprintf("%.0f", a)
		}
		return strings.Join(parts, " | ")
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
	w.Resize(fyne.NewSize(800, 600))

	var fileA string
	var fileB string
	var outputDir string

	fileALabel := widget.NewLabel("Not selected")
	fileBLabel := widget.NewLabel("Not selected")
	outputDirLabel := widget.NewLabel("Same as File A location")

	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	logText := widget.NewTextGrid()
	logText.SetText("Ready to start...\n")

	logWriter := func(msg string) {
		currentTime := time.Now().Format("15:04:05")
		logText.SetText(logText.Text() + fmt.Sprintf("[%s] %s\n", currentTime, msg))
	}

	selectFileA := widget.NewButton("Select File A", func() {
		dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if r == nil {
				return
			}
			fileA = r.URI().Path()
			fileALabel.SetText(filepath.Base(fileA))

			if outputDir == "" {
				outputDir = filepath.Dir(fileA)
				outputDirLabel.SetText(outputDir)
			}

			logWriter(fmt.Sprintf("File A: %s", filepath.Base(fileA)))
		}, w).Show()
	})

	selectFileB := widget.NewButton("Select File B", func() {
		dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if r == nil {
				return
			}
			fileB = r.URI().Path()
			fileBLabel.SetText(filepath.Base(fileB))
			logWriter(fmt.Sprintf("File B: %s", filepath.Base(fileB)))
		}, w).Show()
	})

	selectOutputDir := widget.NewButton("Select Output Directory", func() {
		dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
			if dir == nil {
				return
			}
			outputDir = dir.Path()
			outputDirLabel.SetText(outputDir)
			logWriter(fmt.Sprintf("Output directory: %s", outputDir))
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
		logText.SetText("")
		logWriter("Starting reconciliation process...")

		if outputDir == "" {
			outputDir = filepath.Dir(fileA)
			outputDirLabel.SetText(outputDir)
		}

		go func() {
			timestamp := time.Now().Format("20060102_150405")

			progressBar.SetValue(0.1)
			logWriter("Step 1/5: Reading File A...")

			dataA, err := excelToJSON(fileA)
			if err != nil {
				dialog.ShowError(fmt.Errorf("error reading File A: %v", err), w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			jsonPathA := filepath.Join(outputDir, fmt.Sprintf("file_a_data_%s.json", timestamp))
			saveJSON(dataA, jsonPathA)
			logWriter(fmt.Sprintf("File A: %d transactions extracted", len(dataA.Transactions)))

			progressBar.SetValue(0.4)
			logWriter("Step 2/5: Reading File B...")

			dataB, err := excelToJSON(fileB)
			if err != nil {
				dialog.ShowError(fmt.Errorf("error reading File B: %v", err), w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			jsonPathB := filepath.Join(outputDir, fmt.Sprintf("file_b_data_%s.json", timestamp))
			saveJSON(dataB, jsonPathB)
			logWriter(fmt.Sprintf("File B: %d transactions extracted", len(dataB.Transactions)))

			progressBar.SetValue(0.7)
			logWriter("Step 3/5: Comparing transactions...")

			results := reconcile(dataA, dataB)

			progressBar.SetValue(0.85)
			logWriter(fmt.Sprintf("Step 4/5: Found %d dates with discrepancies", len(results)))

			if len(results) == 0 {
				logWriter("No discrepancies found - files are identical!")
				progressBar.SetValue(1.0)
				time.Sleep(1 * time.Second)
				progressBar.Hide()
				runButton.Enable()
				dialog.ShowInformation("Complete", "No discrepancies found!", w)
				return
			}

			logWriter("Step 5/5: Generating output files...")

			jsonReportPath := filepath.Join(outputDir, fmt.Sprintf("reconciliation_report_%s.json", timestamp))
			generateJSONReport(results, jsonReportPath)
			logWriter(fmt.Sprintf("JSON report saved: %s", filepath.Base(jsonReportPath)))

			excelReportPath := filepath.Join(outputDir, fmt.Sprintf("reconciliation_report_%s.xlsx", timestamp))
			err = generateExcelReport(results, excelReportPath)
			if err != nil {
				dialog.ShowError(fmt.Errorf("error generating Excel report: %v", err), w)
				runButton.Enable()
				progressBar.Hide()
				return
			}

			progressBar.SetValue(1.0)
			logWriter(fmt.Sprintf("Excel report saved: %s", filepath.Base(excelReportPath)))
			logWriter("Reconciliation completed successfully!")

			summary := fmt.Sprintf("Total transactions:\n"+
				"  File A: %d\n"+
				"  File B: %d\n\n"+
				"Dates with discrepancies: %d",
				len(dataA.Transactions),
				len(dataB.Transactions),
				len(results))

			time.Sleep(500 * time.Millisecond)
			progressBar.Hide()
			runButton.Enable()

			dialog.ShowInformation("Reconciliation Complete", summary, w)
		}()
	}

	fileSelection := widget.NewCard("File Selection", "",
		container.NewVBox(
			container.NewHBox(
				widget.NewLabel("File A:"),
				fileALabel,
			),
			selectFileA,
			widget.NewSeparator(),
			container.NewHBox(
				widget.NewLabel("File B:"),
				fileBLabel,
			),
			selectFileB,
			widget.NewSeparator(),
			container.NewHBox(
				widget.NewLabel("Output:"),
				outputDirLabel,
			),
			selectOutputDir,
		),
	)

	actionPanel := widget.NewCard("Actions", "",
		container.NewVBox(
			runButton,
			progressBar,
		),
	)

	logPanel := widget.NewCard("Process Log", "",
		logText,
	)

	leftPanel := container.NewVBox(
		fileSelection,
		actionPanel,
	)

	content := container.NewHSplit(leftPanel, logPanel)
	content.SetOffset(0.4)

	w.SetContent(content)
	w.ShowAndRun()
}
