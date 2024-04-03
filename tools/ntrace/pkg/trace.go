package trace

import (
	"fmt"
	"os"
	"time"

	svg "github.com/ajstarks/svgo"
)

type Point struct {
	Node       string
	Timestamps time.Time
}

type Line struct {
	To, From Point
	Type     string
}

type Lines []Line

func (l Lines) UniqueNodesCount() int {
	nodeSet := make(map[string]struct{})
	for _, line := range l {
		nodeSet[line.To.Node] = struct{}{}
		nodeSet[line.From.Node] = struct{}{}
	}
	return len(nodeSet)
}

func (l Lines) LongestNodeSize() int {
	maxLength := 0
	for _, line := range l {
		toLength := len(line.To.Node)
		fromLength := len(line.From.Node)
		if toLength > maxLength {
			maxLength = toLength
		}
		if fromLength > maxLength {
			maxLength = fromLength
		}
	}
	return maxLength
}

func (l Lines) TimeRange() (time.Time, time.Time) {
	if len(l) == 0 {
		return time.Time{}, time.Time{} // Return zero times if no lines exist
	}
	first := l[0].From.Timestamps // Initialize with the first line's from timestamp
	last := l[0].To.Timestamps    // Initialize with the first line's to timestamp

	for _, line := range l {
		if line.From.Timestamps.Before(first) {
			first = line.From.Timestamps
		}
		if line.To.Timestamps.After(last) {
			last = line.To.Timestamps
		}
	}
	return first, last
}

func (l Lines) ColourMap() map[string]string {
	colourMap := make(map[string]string)
	colorIndex := 0

	for _, line := range l {
		if _, exists := colourMap[line.Type]; !exists {
			colourMap[line.Type] = colours[colorIndex%len(colours)]
			colorIndex++
		}
	}
	return colourMap
}

type Message struct {
	ID        string // must be unique
	Node      string
	Type      string
	Timestamp time.Time
}

func (m Message) IsBefore(other Message) bool {
	return m.Timestamp.Before(other.Timestamp)
}

func (m Message) ToPoint() Point {
	return Point{
		Node:       m.Node,
		Timestamps: m.Timestamp,
	}
}

type Messages []Message

func (m Messages) ToLines() (Lines, error) {
	byID := make(map[string][]Message)
	for _, message := range m {
		byID[message.ID] = append(byID[message.ID], message)
	}
	lines := make([]Line, 0, len(byID))

	for _, messageSet := range byID {
		if len(messageSet) != 2 {
			return nil, fmt.Errorf("message set %v has %d messages, expected 2 per ID", messageSet, len(messageSet))
		}

		if messageSet[0].Type != messageSet[1].Type {
			return nil, fmt.Errorf("message set %v has different types, expected same type", messageSet)
		}

		var from, to Message
		if messageSet[0].IsBefore(messageSet[1]) {
			from = messageSet[0]
			to = messageSet[1]
		} else {
			from = messageSet[1]
			to = messageSet[0]
		}

		lines = append(lines, Line{
			To:   to.ToPoint(),
			From: from.ToPoint(),
			Type: messageSet[0].Type,
		})
	}
	return lines, nil
}

func Generate(output string, m Messages) error {
	lines, err := m.ToLines()
	if err != nil {
		return err
	}
	return GenerateFromLines(output, lines)
}

func GenerateFromLines(output string, lines Lines) error {
	if len(lines) > 1000 {
		return fmt.Errorf("a maximum of 1000 lines are supported, got %d", len(lines))
	}
	file, err := os.Create(fmt.Sprintf("%s.svg", output))
	if err != nil {
		return err
	}
	defer file.Close()

	margin := 20
	startTime, endTime := lines.TimeRange()
	diff := endTime.Sub(startTime)
	// interval := diff / 20
	numNodes := lines.UniqueNodesCount()
	if numNodes > 10 {
		return fmt.Errorf("a maximum of 10 nodes are supported, got %d", numNodes)
	}
	nodeSize := lines.LongestNodeSize()
	lineIndent := nodeSize*8 + margin + 10
	span := 990 - lineIndent
	diffPerPixel := int(diff) / span
	windowHeight := numNodes*30 + margin*2 + 100
	colourMap := lines.ColourMap()

	canvas := svg.New(file)
	canvas.Start(1000, windowHeight) // Assuming a canvas size, this may need to be adjusted

	nodePositions := make(map[string]int)
	currentHeight := margin // Starting height for the first node

	// Draw horizontal lines for each unique node, label them, and record their Y positions
	for _, line := range lines {
		if _, exists := nodePositions[line.From.Node]; !exists {
			canvas.Line(lineIndent, currentHeight, 990, currentHeight, "stroke:black")       // Drawing a line across the canvas
			canvas.Text(10, currentHeight+5, line.From.Node, "text-anchor:start;fill:black") // Labeling the line with the node name
			nodePositions[line.From.Node] = currentHeight
			currentHeight += 30 // Incrementing the height for the next node line
		}
		if _, exists := nodePositions[line.To.Node]; !exists {
			canvas.Line(lineIndent, currentHeight, 990, currentHeight, "stroke:black")     // Drawing a line across the canvas
			canvas.Text(10, currentHeight+5, line.To.Node, "text-anchor:start;fill:black") // Labeling the line with the node name
			nodePositions[line.To.Node] = currentHeight
			currentHeight += 30 // Incrementing the height for the next node line
		}
	}

	legendHeight := currentHeight + 20 // Starting height for the legend, 20 pixels below the last node
	canvas.Text(10, legendHeight, "Legend:", "text-anchor:start;fill:black")
	legendItemHeight := legendHeight + 20 // Starting height for the first legend item
	for lineType, colour := range colourMap {
		canvas.Line(10, legendItemHeight, 30, legendItemHeight, fmt.Sprintf("stroke:%s", colour)) // Drawing a coloured line for the legend
		canvas.Text(35, legendItemHeight+5, lineType, "text-anchor:start;fill:black")             // Labeling the line with the line type
		legendItemHeight += 20                                                                    // Incrementing the height for the next legend item
	}

	// Draw diagonal lines between nodes based on the timestamps and add a small dot at the start and end of each line
	for _, line := range lines {
		fromY := nodePositions[line.From.Node]
		toY := nodePositions[line.To.Node]
		// Assuming timestamp can somehow be converted to an X coordinate, this logic may need to be adjusted
		fromX := int(line.From.Timestamps.Sub(startTime))/diffPerPixel + lineIndent         // Ensuring the line starts within canvas bounds
		toX := int(line.To.Timestamps.Sub(startTime))/diffPerPixel + lineIndent             // Ensuring the line ends within canvas bounds
		canvas.Line(fromX, fromY, toX, toY, fmt.Sprintf("stroke:%s", colourMap[line.Type])) // Drawing a line from one node to another
		canvas.Circle(fromX, fromY, 3, fmt.Sprintf("fill:%s", colourMap[line.Type]))        // Adding a small dot at the start of the line
		canvas.Circle(toX, toY, 3, fmt.Sprintf("fill:%s", colourMap[line.Type]))            // Adding a small dot at the end of the line
	}

	canvas.End()
	return nil
}

var colours = []string{"red", "blue", "green", "yellow", "purple", "orange", "pink", "teal", "grey", "maroon"}
