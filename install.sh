#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bin_dir=${XDG_BIN_HOME:-"$HOME/.local/bin"}
apps_dir=${XDG_DATA_HOME:-"$HOME/.local/share"}/applications
icons_dir=${XDG_DATA_HOME:-"$HOME/.local/share"}/icons/hicolor/scalable/apps

for command_name in go docker python3 zenity xdg-open; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Missing required command: %s\n' "$command_name" >&2
    exit 1
  fi
done
docker compose version >/dev/null
python3 -c 'import gi; gi.require_version("AyatanaAppIndicator3", "0.1"); from gi.repository import AyatanaAppIndicator3, Gtk' 2>/dev/null || {
  printf '%s\n' 'Missing native tray support: install python3-gi and gir1.2-ayatanaappindicator3-0.1' >&2
  exit 1
}

mkdir -p "$bin_dir" "$apps_dir" "$icons_dir"
(cd "$project_dir" && go build -o "$bin_dir/drillbox" .)
install -m 0644 "$project_dir/static/drillbox.svg" "$icons_dir/drillbox.svg"

# Remove launcher artifacts from the former Read / Run name. Course progress
# and Grafana state use separate storage and are intentionally preserved.
rm -f "$bin_dir/read-run" "$apps_dir/read-run.desktop" "$icons_dir/read-run.svg"

desktop_file="$apps_dir/drillbox.desktop"
{
  printf '%s\n' '[Desktop Entry]'
  printf '%s\n' 'Type=Application'
  printf '%s\n' 'Version=1.0'
  printf '%s\n' 'Name=Drillbox'
  printf '%s\n' 'Comment=Practice programming and observability locally'
  printf 'Exec=%s --launcher\n' "$bin_dir/drillbox"
  printf 'TryExec=%s\n' "$bin_dir/drillbox"
  printf 'Path=%s\n' "$project_dir"
  printf '%s\n' 'Icon=drillbox'
  printf '%s\n' 'Terminal=false'
  printf '%s\n' 'Categories=Education;'
  printf '%s\n' 'StartupNotify=true'
} > "$desktop_file"
chmod 0644 "$desktop_file"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$apps_dir" >/dev/null 2>&1 || true
fi

printf 'Installed Drillbox. Open it from your application menu.\n'
