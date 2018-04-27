package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

func InteractiveSelection(c io.ReadWriter, prompt string, acl SSHConfigACL) (string, error) {
	t := terminal.NewTerminal(c, "Please Enter A Server ID: ")
	var choices []string

	generateChoice := func() {
		fmt.Fprintf(c, "%s\r\n", prompt)
		choices = acl.GetServerChoices()
		for i, v := range choices {
			fmt.Fprintf(c, "    [ %2d ] %s\r\n", i+1, v)
		}

		// t.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		// 	bagSizes := []int{2}
		// 	cm := closestmatch.New(choices, bagSizes)
		// 	// log.Println("Find ClosestN", "+", line)
		// 	chose := cm.ClosestN(line, 10)
		// 	// log.Println("ClosestN", "+", chose)
		// 	if len(chose) > 0 {
		// 		// log.Println("ClosestN1")
		// 		fmt.Fprintf(c, "\r\n")
		// 		for _, cs := range chose {
		// 			fmt.Fprintf(c, "chose: %s\r", cs)
		// 		}
		// 		return line, pos, false
		// 	}
		// 	return line, pos, false
		// }
	}

	generateChoice()
	var ct int = 0
	for {
		// Only allow a maxmimum of 3 attempts.
		if ct > 3 {
			fmt.Fprintf(c, "Maximum Number of Attempts Reached\r\n")
			return "", fmt.Errorf("Maximum Number of Attempts Reached")
		} else {
			ct += 1
		}

		sel, err := t.ReadLine()
		if err != nil {
			return "", err
		}

		// log.Printf("Input: %s, %v", sel, cap(serverFreshForce))
		switch strings.TrimSpace(sel) {
		case "r":
			done := make(chan bool)
			serverFreshForce <- refreshServerChan{
				Done: done,
			}
			<-done

			ct = 0
			//clear screen
			t.Write([]byte("\033[2J"))
			generateChoice()
			continue
		case "exit":
			ct = 4
			continue
		}

		i, err := strconv.Atoi(sel)
		if err != nil {
			continue
		}

		if (i < 0) || (i > len(choices)) {
			continue
		} else {
			return choices[(i - 1)], err
		}
	}
}
