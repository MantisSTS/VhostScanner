# VhostScanner

## How to use
This tool ingests the results from `tlsx` and finds valid vhosts running a web application.

## Usage
```
$ cat targets.txt | tlsx -cn -san -o output.txt
$ ./VhostScanner -f ./output.txt -v -b | tee -a results.txt
```
