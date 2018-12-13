package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

func InteractiveSelection(c io.ReadWriter, prompt string, acl SSHConfigACL) (string, error) {

	t := NewTerminal(c, "\r\nPlease Enter A Server ID: ")
	var choices []string

	generateChoice := func() {
		t.handleKey(keyClearScreen)
		fmt.Fprintf(t, "%s", prompt)
		choices = acl.GetServerChoices()
		for i, v := range choices {
			fmt.Fprintf(t, "    [ %2d ] %s", i+1, v)
		}

		// t.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		// 	bagSizes := []int{4}
		// 	cm := closestmatch.New(choices, bagSizes)
		// 	log.Println("AutoCompleteCallback Pos: ", "+", pos, line)
		// 	// log.Println("Find ClosestN", "+", line)
		// 	chose := cm.ClosestN(line, 10)

		// 	if len(chose) > 0 {
		// 		log.Println("AutoCompleteCallback 2: ", "+", line, chose)
		// 		line = fmt.Sprintf("%s\r\n%s", line, strings.Join(chose[:], "\r\n"))
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
			done := make(chan bool, 1)
			serverFreshForce <- refreshServerChan{
				Done: done,
			}
			<-done

			ct = 0
			// clear screen

			// t.Write([]byte("\033[2J"))

			time.Sleep(time.Duration(1) * time.Second)
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

		if (i <= 0) || (i > len(choices)) {
			continue
		} else {
			return choices[(i - 1)], err
		}
	}
}
