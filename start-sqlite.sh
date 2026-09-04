#!/usr/bin/env bash

set -euo pipefail

prompt_default() {
	local prompt="$1"
	local default="$2"
	local value

	read -r -p "$prompt [$default]: " value
	printf '%s' "${value:-$default}"
}

echo "Запуск check-flight с SQLite"
echo

provider="$(prompt_default "Провайдер" "svo")"

echo "Направление:"
echo "  1) Все"
echo "  2) Вылеты"
echo "  3) Прилеты"
direction_choice="$(prompt_default "Выберите вариант" "1")"
direction=""
case "$direction_choice" in
	1) direction="" ;;
	2) direction="departure" ;;
	3) direction="arrival" ;;
	*) echo "Некорректное направление" >&2; exit 1 ;;
esac

search="$(prompt_default "Город или фильтр поиска" "")"
terminal="$(prompt_default "Терминал" "")"

echo "Вывод обновлений рейсов в консоль?"
echo "  1) Да"
echo "  2) Нет"
printing_choice="$(prompt_default "Выберите вариант" "1")"

args=("--provider" "$provider")
if [[ -n "$direction" ]]; then
	args+=("--direction" "$direction")
fi
if [[ -n "$search" ]]; then
	args+=("--search" "$search")
fi
if [[ -n "$terminal" ]]; then
	args+=("--terminal" "$terminal")
fi
case "$printing_choice" in
	1) ;;
	2) args+=("--no-printing") ;;
	*) echo "Некорректный вариант вывода" >&2; exit 1 ;;
esac

if [[ -n "${TELEGRAM_BOT_TOKEN:-}" ]]; then
	echo "Используется TELEGRAM_BOT_TOKEN из окружения."
else
	read -r -s -p "Telegram Bot Token (Enter для консольного режима): " token
	echo
	if [[ -n "$token" ]]; then
		args+=("--token" "$token")
	fi
fi

echo
printf 'Запуск: go run cmd/tracker/main.go'
printf ' %q' "${args[@]}"
echo
echo

DB_DRIVER=sqlite DATABASE_URL=./flights.db go run cmd/tracker/main.go "${args[@]}"
