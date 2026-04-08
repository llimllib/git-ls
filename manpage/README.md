# Man Page Setup

This directory contains the man page for git-ls.

## Files

- `git-ls.1` - The man page in roff format

## Testing Locally

To test the man page:

```bash
man ./git-ls.1
```

To install it locally (so `git ls --help` works):

```bash
mkdir -p ~/.local/share/man/man1
cp git-ls.1 ~/.local/share/man/man1/
```

Then `git ls --help` should display the man page.

## Homebrew Installation

When you release via goreleaser, the man page will automatically be:
1. Included in release tarballs (see `.goreleaser.yaml`)
2. Installed to `/opt/homebrew/share/man/man1/` via the Homebrew formula

Users who install via `brew install llimllib/git-ls/git-ls` will automatically get the man page, and `git ls --help` will work correctly.

## Updating the Man Page

The man page is written in roff format. Key macros:

- `.TH` - Title header (page title, section, date, etc.)
- `.SH` - Section header
- `.TP` - Tagged paragraph (for options)
- `.B` - Bold text
- `.I` - Italic text
- `.BI` - Bold-italic (mixed)
- `.BR` - Bold-roman (mixed)
- `.PP` - Paragraph break

Test your changes with `man ./git-ls.1` before committing.
