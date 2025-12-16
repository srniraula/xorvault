# Files Directory

This directory is for storing test files (PDFs, documents, etc.) that you want to upload to the DFS.

## Usage

Place your files here:
```
files/
  test.pdf
  document.txt
  big.pdf
```

Then upload them:
```bash
make upload FILE=test.pdf
# Client will automatically look in files/ directory
```

Or specify the full path:
```bash
make upload FILE=files/test.pdf
```

## Note

- Files in this directory are ignored by git (except this README)
- Downloaded files are saved to the project root with `downloaded_` prefix
