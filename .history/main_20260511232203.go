package main

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/xuri/excelize/v2"
)

type Transaction struct {
	Date   string
	Amount float64
}

type DailySummary struct {
	Date      string
	CountA    int
	SumA      float64
	CountB    int
	SumB      float64
	DiffCount int
	DiffSum   float64
}

func readExcelData(path string, dateCol, amountCol int) (map[string][]float64, error) {

	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		return nil, err
	}

	data := make(map[string][]float64)

	for i, row := range rows {

		if i == 0 {
			continue
		}

		if len(row) <= amountCol {
			continue
		}

		date := strings.TrimSpace(row[dateCol])

		amountStr := strings.ReplaceAll(row[amountCol], ",", "")
		amountStr = strings.ReplaceAll(amountStr, " ", "")

		amount, _ := strconv.ParseFloat(amountStr, 64)

		if date != "" {
			data[date] = append(data[date], amount)
		}
	}

	return data, nil
}

func sum(arr []float64) float64 {

	total := 0.0

	for _, v := range arr {
		total += v
	}

	return total
}

func reconcile(a, b map[string][]float64) map[string]*DailySummary {

	report := make(map[string]*DailySummary)
	allDates := map[string]bool{}

	for d := range a {
		allDates[d] = true
	}

	for d := range b {
		allDates[d] = true
	}

	for d := range allDates {

		s := &DailySummary{Date: d}

		if v, ok := a[d]; ok {
			s.CountA = len(v)
			s.SumA = sum(v)
		}

		if v, ok := b[d]; ok {
			s.CountB = len(v)
			s.SumB = sum(v)
		}

		s.DiffCount = s.CountA - s.CountB
		s.DiffSum = s.SumA - s.SumB

		report[d] = s
	}

	return report
}

func generateReport(report map[string]*DailySummary, path string) error {

	f := excelize.NewFile()

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
		f.SetCellValue("Sheet1", cell, h)
	}

	row := 2

	for _, r := range report {

		if r.DiffCount == 0 && math.Abs(r.DiffSum) < 0.01 {
			continue
		}

		f.SetCellValue("Sheet1", fmt.Sprintf("A%d", row), r.Date)
		f.SetCellValue("Sheet1", fmt.Sprintf("B%d", row), r.CountA)
		f.SetCellValue("Sheet1", fmt.Sprintf("C%d", row), r.CountB)
		f.SetCellValue("Sheet1", fmt.Sprintf("D%d", row), r.SumA)
		f.SetCellValue("Sheet1", fmt.Sprintf("E%d", row), r.SumB)
		f.SetCellValue("Sheet1", fmt.Sprintf("F%d", row), r.DiffCount)
		f.SetCellValue("Sheet1", fmt.Sprintf("G%d", row), r.DiffSum)

		row++
	}

	return f.SaveAs(path)
}

func main() {

	a := app.New()
	w := a.NewWindow("Excel Reconciler")
	w.Resize(fyne.NewSize(500, 200))

	var fileA string
	var fileB string

	fileALabel := widget.NewLabel("File A: not selected")
	fileBLabel := widget.NewLabel("File B: not selected")

	selectA := widget.NewButton("Select File A", func() {

		dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {

			if rc == nil {
				return
			}

			fileA = rc.URI().Path()
			fileALabel.SetText(fileA)

		}, w).Show()

	})

	selectB := widget.NewButton("Select File B", func() {

		dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {

			if rc == nil {
				return
			}

			fileB = rc.URI().Path()
			fileBLabel.SetText(fileB)

		}, w).Show()

	})

	run := widget.NewButton("Run Reconciliation", func() {

		if fileA == "" || fileB == "" {
			dialog.ShowInformation("Error", "Select both files", w)
			return
		}

		dataA, err := readExcelData(fileA, 0, 1)
		if err != nil {
			log.Println(err)
			return
		}

		dataB, err := readExcelData(fileB, 0, 1)
		if err != nil {
			log.Println(err)
			return
		}

		report := reconcile(dataA, dataB)

		err = generateReport(report, "reconciliation_result.xlsx")
		if err != nil {
			log.Println(err)
			return
		}

		dialog.ShowInformation("Done", "Report saved as reconciliation_result.xlsx", w)

	})

	content := container.NewVBox(
		fileALabel,
		selectA,
		fileBLabel,
		selectB,
		run,
	)

	w.SetContent(content)

	w.ShowAndRun()
}
