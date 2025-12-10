package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"link-checker/internal/model"
	"link-checker/internal/report/pdf"
)

func main() {
	generateFlag := flag.Bool("generate", false, "Generate sample JSON instead of converting")
	sizeFlag := flag.String("size", "medium", "Size of generated sample: small, medium, large")
	flag.Parse()

	if *generateFlag {
		if err := generateSampleJSON(*sizeFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Обработка сигналов для корректного завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Читаем JSON из stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	// Парсим JSON
	var linkSets []model.LinkSet
	if err := json.Unmarshal(data, &linkSets); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// Создаем билдер отчета
	builder := pdf.NewBuilder(pdf.Config{})

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	// Строим отчет
	report, err := builder.Build(context.Background(), linkSets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error build PDF: %v\n", err)
		os.Exit(1)
	}

	// Копируем PDF в stdout
	if _, err := os.Stdout.Write(report.Body); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing PDF: %v\n", err)
		os.Exit(1)
	}
}

// generateSampleJSON генерирует пример JSON для тестирования
func generateSampleJSON(size string) error {
	var linkSets []model.LinkSet

	switch size {
	case "small":
		linkSets = []model.LinkSet{
			{
				ID: 1,
				Links: []model.Link{
					{
						Name:       "Google",
						URL:        "https://google.com",
						Available:  true,
						StatusCode: 200,
						Reason:     "",
					},
					{
						Name:       "Broken Link",
						URL:        "https://example.com/nonexistent",
						Available:  false,
						StatusCode: 404,
						Reason:     "Page not found",
					},
				},
			},
		}
	case "medium":
		linkSets = []model.LinkSet{
			{
				ID:    101,
				Links: generateLinks(10, 101, false),
			},
			{
				ID:    102,
				Links: generateLinks(8, 102, true),
			},
		}
	case "large":
		linkSets = []model.LinkSet{
			{
				ID:    1001,
				Links: generateLinks(25, 1001, false),
			},
			{
				ID:    1002,
				Links: generateLinks(20, 1002, false),
			},
			{
				ID:    1003,
				Links: generateLinks(30, 1003, true),
			},
			{
				ID:    1004,
				Links: generateLinks(15, 1004, false),
			},
		}
	default:
		return fmt.Errorf("unknown size: %s. Use small, medium or large", size)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(linkSets)
}

func generateLinks(count, id int, withErrors bool) []model.Link {
	links := []model.Link{
		{
			Name:       "GitHub",
			URL:        "https://github.com",
			Available:  true,
			StatusCode: 200,
			Reason:     "",
		},
		{
			Name:       "Stack Overflow",
			URL:        "https://stackoverflow.com",
			Available:  true,
			StatusCode: 200,
			Reason:     "",
		},
		{
			Name:       "Go Documentation",
			URL:        "https://golang.org/doc",
			Available:  true,
			StatusCode: 200,
			Reason:     "",
		},
		{
			Name:       "Example",
			URL:        "https://example.com",
			Available:  true,
			StatusCode: 200,
			Reason:     "",
		},
	}

	for i := 0; i < count-4; i++ {
		available := true
		statusCode := 200
		reason := ""

		if withErrors && i%3 == 0 {
			available = false
			statusCode = 404
			reason = "Page not found"
		} else if withErrors && i%5 == 0 {
			available = false
			statusCode = 500
			reason = "Internal server error"
		} else if withErrors && i%7 == 0 {
			available = false
			statusCode = 0
			reason = "Connection timeout"
		}

		links = append(links, model.Link{
			Name:       fmt.Sprintf("Link %d-%d", id, i+1),
			URL:        fmt.Sprintf("https://example.com/page/%d/%d", id, i+1),
			Available:  available,
			StatusCode: statusCode,
			Reason:     reason,
		})
	}

	return links
}
