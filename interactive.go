package main

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/closestmatch"
)

func InteractiveSelection(c io.ReadWriter, prompt string, acl SSHConfigACL) (string, error) {

	t := NewTerminal(c, "\r\nPlease Enter A Server ID: ")
	var choices []string

	generateChoice := func() {
		fmt.Fprintf(t, "%s\r\n", prompt)
		choices = acl.GetServerChoices()
		for i, v := range choices {
			fmt.Fprintf(t, "    [ %2d ] %s\r\n", i+1, v)
		}

		t.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
			bagSizes := []int{2}
			cm := closestmatch.New(choices, bagSizes)
			log.Println("AutoCompleteCallback Pos: ", "+", pos, line)
			// log.Println("Find ClosestN", "+", line)
			chose := cm.ClosestN(line, 10)

			if len(chose) > 0 {
				log.Println("AutoCompleteCallback 2: ", "+", line, chose[0])
				line = fmt.Sprintf("%s\r\n%s", line, chose[0])
				return line, pos, false
			}
			return line, pos, false
		}
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
			t.handleKey(keyClearScreen)
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

		if (i < 0) || (i > len(choices)) {
			continue
		} else {
			return choices[(i - 1)], err
		}
	}
}
