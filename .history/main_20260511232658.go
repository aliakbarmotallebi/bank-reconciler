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
	return s
}

func readExcelData(path string, dateCol, amountCol int) (map[string][]float64, error) {

	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	sheets := f.GetSheetList()
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
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
		amountStr := cleanAmount(row[amountCol])

		if date == "" || amountStr == "" {
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

func sum(vals []float64) float64 {

	total := 0.0

	for _, v := range vals {
		total += v
	}

	return total
}

func reconcile(a, b map[string][]float64) []DailySummary {

	allDates := map[string]bool{}

	for d := range a {
		allDates[d] = true
	}

	for d := range b {
		allDates[d] = true
	}

	var dates []string

	for d := range allDates {
		dates = append(dates, d)
	}

	sort.Strings(dates)

	var result []DailySummary

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

	sheet := "Report"
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

	for i, r := range report {

		row := i + 2

		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), r.Date)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), r.CountA)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), r.CountB)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), r.SumA)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), r.SumB)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), r.DiffCount)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), r.DiffSum)
	}

	return f.SaveAs(outputPath)
}

func main() {

	a := app.New()

	w := a.NewWindow("Excel Reconciler")

	w.Resize(fyne.NewSize(700, 500))

	var fileA string
	var fileB string
	var outputFile string

	fileALabel := widget.NewLabel("File A: not selected")
	fileBLabel := widget.NewLabel("File B: not selected")
	outputLabel := widget.NewLabel("Output: auto")

	logBox := widget.NewMultiLineEntry()
	logBox.Disable()
	logBox.SetMinRowsVisible(10)

	appendLog := func(s string) {
		logBox.SetText(logBox.Text + "\n" + s)
	}

	dateColEntry := widget.NewEntry()
	dateColEntry.SetText("0")

	amountColEntry := widget.NewEntry()
	amountColEntry.SetText("1")

	selectA := widget.NewButton("Select File A", func() {

		dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {

			if r == nil {
				return
			}

			fileA = r.URI().Path()

			fileALabel.SetText(fileA)

			appendLog("File A selected")

		}, w).Show()

	})

	selectB := widget.NewButton("Select File B", func() {

		dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {

			if r == nil {
				return
			}

			fileB = r.URI().Path()

			fileBLabel.SetText(fileB)

			appendLog("File B selected")

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

			outputLabel.SetText(outputFile)

			appendLog("Output selected")

		}, w).Show()

	})

	runBtn := widget.NewButton("Run", func() {

		if fileA == "" || fileB == "" {

			dialog.ShowInformation("Error", "Select both files", w)

			return
		}

		if outputFile == "" {

			outputFile = fmt.Sprintf(
				"reconciliation_report_%s.xlsx",
				time.Now().Format("20060102_150405"),
			)

			outputLabel.SetText(outputFile)

			appendLog("Output auto generated: " + outputFile)
		}

		dateCol, _ := strconv.Atoi(dateColEntry.Text)
		amountCol, _ := strconv.Atoi(amountColEntry.Text)

		appendLog("Reading file A")

		dataA, err := readExcelData(fileA, dateCol, amountCol)

		if err != nil {

			dialog.ShowError(err, w)

			return
		}

		appendLog("Reading file B")

		dataB, err := readExcelData(fileB, dateCol, amountCol)

		if err != nil {

			dialog.ShowError(err, w)

			return
		}

		appendLog("Reconciling")

		report := reconcile(dataA, dataB)

		appendLog(fmt.Sprintf("Found %d differences", len(report)))

		err = generateReport(report, outputFile)

		if err != nil {

			dialog.ShowError(err, w)

			return
		}

		appendLog("Report generated")

		dialog.ShowInformation("Done", "Report created successfully", w)

	})

	content := container.NewVBox(

		fileALabel,
		selectA,

		fileBLabel,
		selectB,

		outputLabel,
		selectOutput,

		widget.NewLabel("Date Column Index"),
		dateColEntry,

		widget.NewLabel("Amount Column Index"),
		amountColEntry,

		runBtn,

		widget.NewLabel("Log"),
		logBox,
	)

	w.SetContent(content)

	w.ShowAndRun()
}
