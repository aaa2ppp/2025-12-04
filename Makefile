BIN_DIR ?= ./bin
TMP_DIR ?= ./tmp

GOEXE := $(shell go env GOEXE)
TEST_FLAGS ?=
TEST_FLAGS := $(TEST_FLAGS) -tags=dev

# Кастомные флаги сборки (можно переопределить при вызове make)
GO_BUILD_FLAGS ?=

# Находим все поддиректории в cmd, которые потенциально могут быть бинарниками
CMDS := $(wildcard cmd/*)

# Генерируем список целей для бинарников
BINARIES := $(patsubst cmd/%,$(BIN_DIR)/%,$(CMDS))


MERGE_FILES ?= Makefile go.mod go.sum *.go *.sh *.md *.example
MERGE_EXCLUDE_DIRS ?= .* tmp bak bak[0-9] bin data idea

MERGE_FIND_PARTS   := $(patsubst %,-o -name '%',$(MERGE_FILES))
MERGE_FIND_EXPR    := $(wordlist 2,$(words $(MERGE_FIND_PARTS)),$(MERGE_FIND_PARTS))
MERGE_EXCLUDE_EXPR := $(patsubst %,! -path '*/%/*',$(MERGE_EXCLUDE_DIRS))

# source and destination for merge/patch operations
SRC ?= .
DST ?= 1

REPORT_SIZE ?= medium # small, medium, large


.PHONY: FORCE all deps test build clean run

FORCE:


# Основная цель - собирает все бинарники
all: deps test build

${BIN_DIR}:
	mkdir -p ${TMP_DIR}

${TMP_DIR}:
	mkdir -p ${TMP_DIR}


# Правило для подготовки зависимостей
deps:
	go mod tidy

test:
	go test $(TEST_FLAGS) ./...
	@echo OK


# Шаблонное правило для сборки любого бинарника
$(BIN_DIR)/%: ${BIN_DIR} FORCE
	@mkdir -p $(@D)
	go build $(GO_BUILD_FLAGS) -o $@$(GOEXE) ./cmd/$(notdir $@)

build: $(BINARIES)

test-report: bin/json2pdf $(TMP_DIR)
	bin/json2pdf -generate -size=$(REPORT_SIZE) | bin/json2pdf > $(TMP_DIR)/$(DST).pdf

# Очистка
clean:
	-rm -rf $(BIN_DIR) $(TMP_DIR)

.PHONY: merge patch

merge: ${TMP_DIR}
	@find $(SRC) -type f \( $(MERGE_FIND_EXPR) \) -exec sh -c 'name="{}"; \
		printf "== $${name#./} ==\n\n"; cat "$$name"; echo' ';' > $(TMP_DIR)/$(DST).code
	@echo "Merge saved to $(TMP_DIR)/$(DST).code"	
	

# Создает прекоммит патч
patch: ${TMP_DIR} test
	@(set -e; \
	staged_list="$(TMP_DIR)/staged_list.$$$$"; \
	unstaged_list="$(TMP_DIR)/unstaged_list.$$$$"; \
	git diff --staged --name-only -- $(SRC) > "$$staged_list"; \
	git diff --name-only -- $(SRC) > "$$unstaged_list"; \
	intersection=$$(grep -Fxf "$$staged_list" "$$unstaged_list" || true); \
	rm -f "$$staged_list" "$$unstaged_list"; \
	if [ -n "$$intersection" ]; then \
		echo "" >&2; \
		echo "WARNING: the following files have changes not staged for commit:" >&2; \
		echo "  (use \"git add <file>...\" to update what will be committed)" >&2; \
		printf '%s\n' $$intersection | sed 's/^/        /' >&2; \
		echo "" >&2; \
	fi)
	
	git diff --staged -- $(SRC) > $(TMP_DIR)/$(DST).patch
	@echo "Patch saved to $(TMP_DIR)/$(DST).patch"
