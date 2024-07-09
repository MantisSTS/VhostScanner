# VhostScanner

## How to use
This tool ingests the results from `tlsx` and finds valid vhosts running a web application.

## Usage
```
$ cat targets.txt | tlsx -cn -san -o output.txt
$ ./VhostScanner -f ./output.txt -v -b | tee -a results.txt
```

![image](https://github.com/MantisSTS/VhostScanner/assets/818959/c9e716f7-b7f0-4b04-a87f-101a7321fc42)
