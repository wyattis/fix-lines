# fix-lines
A simple tool to change all crlf to lf

## Usage
```
go install github.com/wyattis/fix-lines@latest

fix-lines --dry-run
```

## Test
```
go build && cp -r testdata tmptestdata && ./fix-lines ./tmptestdata
``` 