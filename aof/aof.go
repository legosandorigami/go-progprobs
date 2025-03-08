package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type aofLog struct {
	op    string
	value int
}

func main() {

	// Check if no arguments are provided
	if len(os.Args) < 2 {
		fmt.Println("no command input specified")
		return
	}

	// Parse command-line arguments
	aofFlag := flag.String("f", "", "append only file that need to be compacted")

	flag.Parse()

	if *aofFlag != "" {
		file, err := os.Open(*aofFlag)
		if err != nil {
			fmt.Println("Error opening file:", err)
			return
		}
		defer file.Close()

		// endPoints maps line number to the key
		endPoints := make(map[int]string)

		// "compactedStore maps the key to aofLog which represents final operation and final value"
		compactedStore := make(map[string]*aofLog)

		scanner := bufio.NewScanner(file)

		// reading the header
		scanner.Scan()
		numOfKeys, err := strconv.Atoi(scanner.Text())
		if err != nil || numOfKeys <= 0 {
			fmt.Println("first line of the file must be a positive integer:", err)
			return
		}

		i := 0
		for scanner.Scan() {
			pieces := strings.Split(scanner.Text(), " ")
			ep, _ := strconv.Atoi(pieces[1])
			endPoints[ep] = pieces[0]
			i += 1
			if i == numOfKeys {
				break
			}
		}

		i = 0

		for scanner.Scan() {
			// reading the rest of the file
			pieces := strings.Split(scanner.Text(), " ")
			op, key := pieces[0], pieces[1]
			var value int
			if len(pieces) == 3 {
				value, _ = strconv.Atoi(pieces[2])
			}
			origLog := compactedStore[key]
			switch op {
			case "CREATE":
				origLog = &aofLog{
					op:    "CREATE",
					value: value,
				}
				compactedStore[key] = origLog
			case "SET":
				origLog.value = value
			case "MODIFY":
				origLog.value += value
			case "DELETE":
				origLog.op = "DELETE"
			}
			if k, ok := endPoints[i]; ok {
				switch origLog.op {
				case "DELETE":
					fmt.Printf("%s %s\n", origLog.op, k)
				default:
					fmt.Printf("%s %s %d\n", origLog.op, k, origLog.value)
				}
			}
			i += 1
		}

		// fmt.Println("Printing endpoints")
		// for lineNo, key := range endPoints {
		// 	fmt.Printf("%s %d\n", key, lineNo)
		// }

		if err := scanner.Err(); err != nil {
			fmt.Println("Error reading file:", err)
		}
	}
}
