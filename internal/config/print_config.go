package config

import (
	"fmt"
	"os"
	"strings"
)

var _, printConfig = os.LookupEnv("PRINT_CONFIG")

func osExitIfPrintConfig() {
	if printConfig {
		os.Exit(1)
	}
}

var envDescriptions = map[string]string{
	"LOG_*":         "Logger",
	"LOG_LEVEL":     "Уровень детализации логов (DEBUG, INFO, WARN, ERROR)",
	"LOG_PLAINTEXT": "Вывод логов в текстовом формате вместо JSON",

	"SERVER_*":                "Server",
	"SERVER_ADDR":             "Адрес и порт для запуска HTTP-сервера",
	"SERVER_SHUTDOWN_TIMEOUT": "Таймаут graceful shutdown сервера",
	"SERVER_RETRY_AFTER":      "Значение заголовка Retry-After в секундах при 503",

	"CHECKER_*":                "Checker",
	"CHECKER_TIMEOUT":          "Таймаут проверки одной ссылки",
	"CHECKER_USER_AGENT":       "User-Agent для HTTP-запросов при проверке ссылок",
	"CHECKER_TRY_HTTPS_FIRST":  "Пробовать сначала HTTPS потом HTTP (иначе наоборот)",
	"CHECKER_TRY_GET_FALLBACK": "Пробовать GET, если HEAD не поддерживается (не реализовано)",

	"BBSTOR_*":                "Storage (BBolt)",
	"BBSTOR_DATA_FILE":        "Путь к файлу базы данных BBolt",
	"BBSTOR_OPEN_TIMEOUT":     "Таймаут открытия базы данных",
	"BBSTOR_CACHE_SIZE":       "Максимальное количество записей в LRU кеше",
	"BBSTOR_NO_SYNC":          "Отключить синхронизацию (ВОЗМОЖНА ПОТЕРЯ ДАННЫХ)",
	"BBSTOR_MAX_SYNC_DELAY":   "Ленивая синхронизация. Устанавливает отставание синхронизации по времени после коммита (ВОЗМОЖНА ПОТЕРЯ ДАННЫХ)",
	"BBSTOR_MAX_SYNC_PENDING": "Ленивая синхронизация. Устанавливает отставание синхронизации по записям после коммита (ВОЗМОЖНА ПОТЕРЯ ДАННЫХ)",
	"BBSTOR_MAX_BATCH_DELAY":  "Пакетная запись. Устанавливает максимальное время комплектации пакета",
	"BBSTOR_MAX_BATCH_SIZE":   "Пакетная запись. Устанавливает максимальный размер пакета",
}

var (
	prevPrefix string
	firstLine  = true
)

func printEnv[T any](key string, val, defVal T, required bool) {
	if !printConfig {
		return
	}

	p := strings.IndexByte(key, '_')
	if p == -1 {
		p = len(key)
	}

	if curPrefix := key[:p]; curPrefix != prevPrefix {
		if !firstLine {
			fmt.Print("\n\n")
		}
		firstLine = false

		fmt.Printf("# === %s ===\n", envDescriptions[curPrefix+"_*"])
		prevPrefix = curPrefix
	}

	if !firstLine {
		fmt.Print("\n")
	}
	firstLine = false

	if required {
		fmt.Printf("# %s - %s (REQUIRED)\n", key, envDescriptions[key])
	} else {
		fmt.Printf("# %s - %s (Default: %v)\n", key, envDescriptions[key], defVal)
	}
	fmt.Printf("%s=%v\n", key, val)
}
