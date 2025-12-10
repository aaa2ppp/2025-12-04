package pdf

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/jung-kurt/gofpdf/v2"

	"link-checker/internal/model"
	"link-checker/internal/service"
)

type Config struct {
	Title        string
	FontSize     float64
	HeaderHeight float64
	Margin       float64
}

type Builder struct {
	cfg Config
}

func (b *Builder) Build(ctx context.Context, linkSets []model.LinkSet) (model.Report, error) {
	// Создаем PDF документ
	pdf := b.createPDF()

	// Добавляем первую страницу с заголовком
	pdf.AddPage()
	b.addHeader(pdf)

	// Добавляем статистику
	b.addStatistics(pdf, linkSets)

	// Добавляем детали по каждому LinkSet
	for _, linkSet := range linkSets {
		b.addLinkSet(pdf, linkSet)
	}

	// Генерируем PDF
	var buf bytes.Buffer
	err := pdf.Output(&buf)
	if err != nil {
		return model.Report{}, err
	}

	return model.Report{
		ContentType: "application/pdf",
		Body:        buf.Bytes(),
	}, nil
}

func (b *Builder) createPDF() *gofpdf.Fpdf {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, b.cfg.Margin)

	// Устанавливаем шрифт (используем встроенный шрифт для простоты)
	pdf.SetFont("Arial", "", b.cfg.FontSize)

	return pdf
}

func (b *Builder) addHeader(pdf *gofpdf.Fpdf) {
	// Заголовок отчета
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, b.cfg.HeaderHeight, b.cfg.Title, "", 1, "C", false, 0, "")

	// Дата генерации
	pdf.SetFont("Arial", "I", 10)
	pdf.CellFormat(0, 10, fmt.Sprintf("Generated: %s", time.Now().Format("2006-01-02 15:04:05")), "", 1, "C", false, 0, "")

	pdf.Ln(10)
}

func (b *Builder) addStatistics(pdf *gofpdf.Fpdf, linkSets []model.LinkSet) {
	totalLinks := 0
	availableLinks := 0

	for _, set := range linkSets {
		totalLinks += len(set.Links)
		for _, link := range set.Links {
			if link.Available {
				availableLinks++
			}
		}
	}

	unavailableLinks := totalLinks - availableLinks
	availabilityRate := 0.0
	if totalLinks > 0 {
		availabilityRate = float64(availableLinks) / float64(totalLinks) * 100
	}

	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(0, 10, "Statistics", "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 10)

	pdf.CellFormat(40, 8, "Total LinkSets:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 8, fmt.Sprintf("%d", len(linkSets)), "", 1, "L", false, 0, "")

	pdf.CellFormat(40, 8, "Total Links:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 8, fmt.Sprintf("%d", totalLinks), "", 1, "L", false, 0, "")

	pdf.CellFormat(40, 8, "Available:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 8, fmt.Sprintf("%d (%.1f%%)", availableLinks, availabilityRate), "", 1, "L", false, 0, "")

	pdf.CellFormat(40, 8, "Unavailable:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 8, fmt.Sprintf("%d", unavailableLinks), "", 1, "L", false, 0, "")

	pdf.Ln(10)
}

func (b *Builder) addLinkSet(pdf *gofpdf.Fpdf, linkSet model.LinkSet) {
	// Проверяем, нужно ли новую страницу
	if pdf.GetY() > 250 {
		pdf.AddPage()
	}

	// Заголовок LinkSet
	pdf.SetFont("Arial", "B", 12)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(0, 10, fmt.Sprintf("LinkSet #%d", linkSet.ID), "", 1, "L", true, 0, "")
	pdf.Ln(5)

	// Таблица с ссылками
	pdf.SetFont("Arial", "B", 10)
	pdf.SetFillColor(220, 220, 220)

	// Заголовки таблицы
	pdf.CellFormat(10, 8, "#", "1", 0, "C", true, 0, "")
	pdf.CellFormat(40, 8, "Name", "1", 0, "L", true, 0, "")
	pdf.CellFormat(60, 8, "URL", "1", 0, "L", true, 0, "")
	pdf.CellFormat(10, 8, "SC", "1", 0, "C", true, 0, "")
	pdf.CellFormat(70, 8, "Reason", "1", 1, "C", true, 0, "")

	pdf.SetFont("Arial", "", 9)

	for i, link := range linkSet.Links {
		// Определяем цвет строки в зависимости от статуса
		if pdf.GetY() > 270 {
			pdf.AddPage()
			// Повторяем заголовки на новой странице
			pdf.SetFont("Arial", "B", 10)
			pdf.SetFillColor(220, 220, 220)

			pdf.CellFormat(10, 8, "#", "1", 0, "C", true, 0, "")
			pdf.CellFormat(40, 8, "Name", "1", 0, "L", true, 0, "")
			pdf.CellFormat(60, 8, "URL", "1", 0, "L", true, 0, "")
			pdf.CellFormat(10, 8, "SC", "1", 0, "C", true, 0, "")
			pdf.CellFormat(70, 8, "Reason", "1", 1, "C", true, 0, "")

			pdf.SetFont("Arial", "", 9)
		}

		fill := false
		if !link.Available {
			pdf.SetFillColor(255, 230, 230) // Светло-красный для недоступных
			fill = true
		} else {
			pdf.SetFillColor(255, 255, 255)
		}

		pdf.CellFormat(10, 8, fmt.Sprintf("%d", i+1), "1", 0, "C", fill, 0, "")
		pdf.CellFormat(40, 8, b.truncateString(link.Name, 20), "1", 0, "L", fill, 0, "")
		pdf.CellFormat(60, 8, b.truncateString(link.URL, 30), "1", 0, "L", fill, 0, "")

		// Код статуса
		statusCode := fmt.Sprintf("%d", link.StatusCode)
		if link.StatusCode == 0 {
			statusCode = "N/A"
		}

		// Добавляем причину ошибки если есть
		if link.Reason != "" {
			pdf.CellFormat(10, 8, statusCode, "1", 0, "C", fill, 0, "")
			pdf.SetFont("Arial", "I", 8)
			pdf.SetTextColor(150, 0, 0)
			pdf.CellFormat(70, 8, b.truncateString(link.Reason, 45), "1", 1, "L", fill, 0, "")
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "", 9)
		} else {
			pdf.CellFormat(10, 8, statusCode, "1", 0, "C", fill, 0, "")
			pdf.CellFormat(70, 8, "", "1", 1, "L", fill, 0, "")
		}
	}

	pdf.Ln(10)
}

func (b *Builder) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func NewBuilder(cfg Config) *Builder {
	// Устанавливаем значения по умолчанию
	if cfg.Title == "" {
		cfg.Title = "Link Checker Report"
	}
	if cfg.FontSize == 0 {
		cfg.FontSize = 12
	}
	if cfg.HeaderHeight == 0 {
		cfg.HeaderHeight = 15
	}
	if cfg.Margin == 0 {
		cfg.Margin = 20
	}

	return &Builder{
		cfg: cfg,
	}
}

var _ service.ReportBuilder = &Builder{}
