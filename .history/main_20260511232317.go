package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/xuri/excelize/v2"
)

type DailySummary struct {
	Date      string
	CountA    int
	SumA      float64
	CountB    int
	SumB      float64
	DiffCount int
	DiffSum   float64
}

func cleanAmount(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "(", "-")
	s = strings.ReplaceAll(s, ")", "")
	return s
}

func readExcelData(path string, dateCol, amountCol int) (map[string][]float64, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", path, err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("file %s has no sheet", path)
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", path, err)
	}

	data := make(map[string][]float64)

	for i, row := range rows {
		if i == 0 {
			continue
		}
		if len(row) <= dateCol || len(row) <= amountCol {
			continue
		}

		date := strings.TrimSpace(row[dateCol])
		if date == "" {
			continue
		}

		amountStr := cleanAmount(row[amountCol])
		if amountStr == "" {
			continue
		}

		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			continue
		}

		data[date] = append(data[date], amount)
	}

	return data, nil
}

func sum(values []float64) float64 {
	total := 0.0
	for _, v := range values {
		total += v
	}
	return total
}

func reconcile(a, b map[string][]float64) []DailySummary {
	allDates := make(map[string]struct{})

	for d := range a {
		allDates[d] = struct{}{}
	}
	for d := range b {
		allDates[d] = struct{}{}
	}

	dates := make([]string, 0, len(allDates))
	for d := range allDates {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	result := make([]DailySummary, 0, len(dates))

	for _, d := range dates {
		item := DailySummary{Date: d}

		if v, ok := a[d]; ok {
			item.CountA = len(v)
			item.SumA = sum(v)
		}
		if v, ok := b[d]; ok {
			item.CountB = len(v)
			item.SumB = sum(v)
		}

		item.DiffCount = item.CountA - item.CountB
		item.DiffSum = item.SumA - item.SumB

		if item.DiffCount != 0 || math.Abs(item.DiffSum) > 0.0001 {
			result = append(result, item)
		}
	}

	return result
}

func generateReport(report []DailySummary, outputPath string) error {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Discrepancies"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{
		"Date",
		"A_Count",
		"B_Count",
		"A_Sum",
		"B_Sum",
		"Diff_Count",
		"Diff_Sum",
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	for i, row := range report {
		r := i + 2
		f.SetCellValue(sheet, fmt.Sprintf("A%d", r), row.Date)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", r), row.CountA)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", r), row.CountB)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", r), row.SumA)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", r), row.SumB)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", r), row.DiffCount)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", r), row.DiffSum)
	}

	return f.SaveAs(outputPath)
}

func main() {
	a := app.New()
	w := a.NewWindow("Excel Reconciler")
	w.Resize(fyne.NewSize(760, 520))

	var fileA string
	var fileB string
	var outputFile string

	fileALabel := widget.NewLabel("File A: not selected")
	fileBLabel := widget.NewLabel("File B: not selected")
	outputLabel := widget.NewLabel("Output: not selected")

	dateColEntry := widget.NewEntry()
	dateColEntry.SetText("0")
	dateColEntry.SetPlaceHolder("Date column index, zero-based")

	amountColEntry := widget.NewEntry()
	amountColEntry.SetText("1")
	amountColEntry.SetPlaceHolder("Amount column index, zero-based")

	logBox := widget.NewMultiLineEntry()
	logBox.SetMinRowsVisible(12)
	logBox.Wrapping = fyne.TextWrapWord
	logBox.Disable()

	appendLog := func(msg string) {
		current := logBox.Text
		if current == "" {
			logBox.SetText(msg)
			return
		}
		logBox.SetText(current + "\n" + msg)
	}

	selectA := widget.NewButton("Select File A", func() {
		dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if rc == nil {
				return
			}
			defer rc.Close()

			fileA = rc.URI().Path()
			fileALabel.SetText("File A: " + fileA)
			appendLog("Selected File A: " + fileA)
		}, w).Show()
	})

	selectB := widget.NewButton("Select File B", func() {
		dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if rc == nil {
				return
			}
			defer rc.Close()

			fileB = rc.URI().Path()
			fileBLabel.SetText("File B: " + fileB)
			appendLog("Selected File B: " + fileB)
		}, w).Show()
	})

	selectOutput := widget.NewButton("Select Output File", func() {
		dialog.NewFileSave(func(uc fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uc == nil {
				return
			}

			outputFile = uc.URI().Path()
			_ = uc.Close()

			if filepath.Ext(outputFile) == "" {
				outputFile += ".xlsx"
			}

			outputLabel.SetText("Output: " + outputFile)
			appendLog("Selected Output: " + outputFile)
		}, w).Show()
	})

	runBtn := widget.NewButton("Run", func() {
		if fileA == "" || fileB == "" {
			dialog.ShowInformation("Validation", "Both input files must be selected.", w)
			return
		}

		if outputFile == "" {
			outputFile = "reconciliation_result.xlsx"
			outputLabel.SetText("Output: " + outputFile)
			appendLog("Output not selected, using default: " + outputFile)
		}

		dateCol, err := strconv.Atoi(strings.TrimSpace(dateColEntry.Text))
		if err != nil || dateCol < 0 {
			dialog.ShowInformation("Validation", "Date column index is invalid.", w)
			return
		}

		amountCol, err := strconv.Atoi(strings.TrimSpace(amountColEntry.Text))
		if err != nil || amountCol < 0 {
			dialog.ShowInformation("Validation", "Amount column index is invalid.", w)
			return
		}

		appendLog("Reading file A...")
		dataA, err := readExcelData(fileA, dateCol, amountCol)
		if err != nil {
			dialog.ShowError(err, w)
			appendLog("Error reading file A: " + err.Error())
			return
		}

		appendLog("Reading file B...")
		dataB, err := readExcelData(fileB, dateCol, amountCol)
		if err != nil {
			dialog.ShowError(err, w)
			appendLog("Error reading file B: " + err.Error())
			return
		}

		appendLog("Reconciling data...")
		report := reconcile(dataA, dataB)

		appendLog(fmt.Sprintf("Found %d discrepancy dates.", len(report)))

		appendLog("Writing output report...")
		err = generateReport(report, outputFile)
		if err != nil {
			dialog.ShowError(err, w)
			appendLog("Error writing output: " + err.Error())
			return
		}

		absPath, _ := filepath.Abs(outputFile)
		appendLog("Done. Report created at: " + absPath)
		dialog.ShowInformation("Completed", "Report generated successfully.", w)
	})

	helpBtn := widget.NewButton("Help", func() {
		helpText := `
Usage:
1. Select File A.
2. Select File B.
3. Optionally select output file.
4. Enter zero-based column indexes:
   - Date column index
   - Amount column index
5. Click Run.

Notes:
- The first sheet in each Excel file is used.
- The first row is treated as header and skipped.
- Date comparison is string-based in this version.
- Amount values are cleaned from commas and spaces before parsing.

Example:
If date is in column A and amount is in column B:
Date column index = 0
Amount column index = 1
`
		dialog.ShowInformation("Help", strings.TrimSpace(helpText), w)
	})

	openOutputBtn := widget.NewButton("Open Output Folder", func() {
		if outputFile == "" {
			dialog.ShowInformation("Info", "No output file selected yet.", w)
			return
		}
		dir := filepath.Dir(outputFile)
		appendLog("Output folder: " + dir)
		dialog.ShowInformation("Output Folder", dir, w)
	})

	resetBtn := widget.NewButton("Reset", func() {
		fileA = ""
		fileB = ""
		outputFile = ""

		fileALabel.SetText("File A: not selected")
		fileBLabel.SetText("File B: not selected")
		outputLabel.SetText("Output: not selected")
		dateColEntry.SetText("0")
		amountColEntry.SetText("1")
		logBox.SetText("")
	})

	form := container.NewVBox(
		fileALabel,
		selectA,
		fileBLabel,
		selectB,
		outputLabel,
		selectOutput,
		widget.NewLabel("Date column index"),
		dateColEntry,
		widget.NewLabel("Amount column index"),
		amountColEntry,
		container.NewHBox(runBtn, helpBtn, openOutputBtn, resetBtn),
		widget.NewLabel("Log"),
		logBox,
	)

	w.SetContent(container.NewPadded(form))
	w.ShowAndRun()

	_ = os.Stdout
}
