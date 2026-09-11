#!/bin/sh
# Prepares the faces an account can wear: `scripts/profiles.sh [source]`.
#
# Nothing on this dashboard draws a face larger than 48 CSS pixels, so
# what is committed is not what came out of the image generator. A
# 2048x2048 PNG is three and a half megabytes, and seven of them is
# twenty-four megabytes in the dashboard's image, over the wire, for an
# icon beside somebody's username. This script is the step between the
# two, and it exists so that step is one command rather than seven
# remembered flags.
#
# It writes **two sizes**, because there are two places a face is drawn
# and they are two orders of magnitude apart:
#
#   <name>.png      the picker on the account screen, 48px at 3x
#   <name>-sm.png   the row at the foot of the sidebar, 24px
#
# The sidebar's is on every screen of the dashboard, which is what makes
# the thumbnail worth a second file: 8 KB against 88 KB, on the one
# image that loads before anything else does.
#
# **The name is not this script's to decide.** A file called
# `Colado 2026-09-11 às 6.41.16.png` is what the generator handed over,
# and what a face should be called is a decision — so a name that could
# not be a name is refused here rather than turned into one. They are
# the palette's names (`cyan`, `mono`, `hacker`, ...) because a face and
# a theme are the same seven colours, and a second vocabulary for them
# would be one to keep in step by hand.
#
# Run with no argument it re-derives the thumbnails from what is already
# committed, which is the idempotent case: the masters are already at
# the size below, so resizing them again changes nothing.
#
# The faces themselves are drawn by `scripts/faces.py`, which is where
# to go to change one. It writes SVG; rasterise that at 256 with a real
# SVG engine and point this at the result.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
profiles="$here/web/public/profiles"
source_dir="${1:-$profiles}"

# The master is the largest anything asks for, doubled and rounded up.
# No larger raster is kept: a megabyte of PNG per face is a megabyte in
# every clone for ever, and nothing here will ever draw one at 2048. The
# source that can be re-cut at any size is `scripts/faces.py`, which is
# a few hundred lines of paths rather than nine binaries.
master=256
thumb=64

# ImageMagick 7, ImageMagick 6, then the one every Mac already has.
if command -v magick >/dev/null 2>&1; then
	resize() { magick "$1" -resize "$3x$3>" -strip "$2"; }
elif command -v convert >/dev/null 2>&1; then
	resize() { convert "$1" -resize "$3x$3>" -strip "$2"; }
elif command -v sips >/dev/null 2>&1; then
	resize() { sips -Z "$3" "$1" --out "$2" >/dev/null; }
else
	echo "profiles: need imagemagick or sips to resize" >&2
	exit 1
fi

mkdir -p "$profiles"
names=""
for src in "$source_dir"/*.png; do
	[ -e "$src" ] || { echo "profiles: no PNGs in $source_dir" >&2; exit 1; }

	name=$(basename "$src" .png)
	case "$name" in
	*-sm) continue ;; # a thumbnail from a previous run
	esac

	# The same rule the daemon applies to what an account may wear, said
	# here so it is caught while the person who chose the name is still
	# holding the file. A name goes into `/profiles/<name>.png` in an
	# <img src> and nowhere else, so anything that is not one is a file
	# the dashboard could not ask for.
	case "$name" in
	*[!a-z0-9-]* | "" | -* | *-)
		echo "profiles: $(basename "$src") is not a name — rename it first (lowercase letters, digits, dashes)" >&2
		exit 1
		;;
	esac

	resize "$src" "$profiles/$name.png" "$master"
	resize "$profiles/$name.png" "$profiles/$name-sm.png" "$thumb"
	names="$names \"$name\","
done

# The other half of the edit. The daemon is what refuses a name, so the
# list has to exist in Go as well — see internal/user/avatars_test.go,
# which fails when this and that disagree. Printing it is as far as this
# goes: rewriting somebody's source file is a worse trade than pasting
# one line.
echo
echo "internal/user/user.go:"
echo "var Avatars = []string{$(echo "$names" | sed 's/^ //; s/,$//')}"
