package rp

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	xmlTimestampLayout = "2006-01-02T15:04:05"
)

// XMLReport identifies JUnit XML format specification that Hudson supports
type XMLReport struct {
	xmlSuites []xmlSuite
}

type xmlSuite struct {
	XMLName     string        `xml:"testsuite"`
	ID          int           `xml:"id,attr"`
	Name        string        `xml:"name,attr"`
	PackageName string        `xml:"package,attr"`
	TimeStamp   string        `xml:"timestamp,attr"`
	Time        float64       `xml:"time,attr"`
	HostName    string        `xml:"hostname,attr"`
	Tests       int           `xml:"tests,attr"`
	Failures    int           `xml:"failures,attr"`
	Errors      int           `xml:"errors,attr"`
	Properties  xmlProperties `xml:"properties"`
	Cases       []xmlTest     `xml:"testcase"`
	SystemOut   string        `xml:"system-out"`
	SystemErr   string        `xml:"system-err"`
}

type xmlProperties struct {
}

type xmlTest struct {
	Name      string      `xml:"name,attr"`
	ClassName string      `xml:"classname,attr"`
	Time      float64     `xml:"time,attr"`
	Failure   *xmlFailure `xml:"failure,omitempty"`
}

type xmlFailure struct {
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Details string `xml:",chardata"`
}

// LoadXMLReport is used for loading JUnit XML report from specified directory
func LoadXMLReport(dirName string) (*XMLReport, error) {
	report, err := parseXMLReport(dirName)
	if err != nil {
		return nil, err
	}
	return &XMLReport{
		xmlSuites: report,
	}, nil
}

// SuitesCount provides suite count for current xml test result report
func (report *XMLReport) SuitesCount() int {
	return len(report.xmlSuites)
}

// TesCaseCount provides test case count for current suite
func (report *XMLReport) TesCaseCount(i int) int {
	return len(report.xmlSuites[i].Cases)
}

// LaunchStartTime is used to calc launch time, it will be equal to 0 suite start time
func (report *XMLReport) LaunchStartTime() time.Time {
	return parseTimeStamp(report.xmlSuites[0].TimeStamp)
}

// LaunchEndTime is used to calc launch end time, it will be equal to last suite start time plus last suite duration
func (report *XMLReport) LaunchEndTime() time.Time {
	lastIndex := len(report.xmlSuites) - 1
	lastSuiteStart := parseTimeStamp(report.xmlSuites[lastIndex].TimeStamp)
	d := secondsToDuration(report.xmlSuites[lastIndex].Time)
	return lastSuiteStart.Add(d)
}

// Suite is used ot create new TestItem type SUITE for xml suite
func (report *XMLReport) Suite(i int) *TestItem {
	xSuite := report.xmlSuites[i]
	suiteStart := parseTimeStamp(xSuite.TimeStamp)
	return &TestItem{
		Type:        TestItemTypeSuite,
		StartTime:   suiteStart,
		Name:        xSuite.PackageName + "." + xSuite.Name,
		Description: fmt.Sprintf("%s %d", TestItemTypeSuite, xSuite.ID),
	}
}

// SuiteResult is used ot create new ExecutionResult for xml suite
func (report *XMLReport) SuiteResult(i int) *ExecutionResult {
	xSuite := report.xmlSuites[i]
	suiteStart := parseTimeStamp(xSuite.TimeStamp)
	d := secondsToDuration(xSuite.Time)
	suiteEnd := suiteStart.Add(d)

	status := ExecutionStatusPassed
	if xSuite.Tests == 0 {
		status = ExecutionStatusSkipped
	} else if xSuite.Failures > 0 {
		status = ExecutionStatusFailed
	} else if xSuite.Errors > 0 {
		status = ExecutionStatusFailed
	}

	return &ExecutionResult{
		EndTime: suiteEnd,
		Status:  status,
	}
}

// TestCase is used ot create new TestItem type STEP for xml test case
func (report *XMLReport) TestCase(i, j int) *TestItem {
	xSuite := report.xmlSuites[i]
	suiteStart := parseTimeStamp(xSuite.TimeStamp)
	xCase := xSuite.Cases[j]
	return &TestItem{
		Type:      TestItemTypeStep,
		Name:      xCase.Name,
		StartTime: suiteStart, // FixMe
	}
}

// TestCaseResult is used ot create new ExecutionResult for xml test case
func (report *XMLReport) TestCaseResult(i, j int) *ExecutionResult {
	xSuite := report.xmlSuites[i]
	suiteStart := parseTimeStamp(xSuite.TimeStamp)
	xCase := xSuite.Cases[j]
	d := secondsToDuration(xCase.Time)
	xCaseEnd := suiteStart.Add(d)
	var status = ExecutionStatusPassed
	if xCase.Failure != nil {
		status = ExecutionStatusFailed
	}

	return &ExecutionResult{
		EndTime: xCaseEnd,
		Status:  status,
	}
}

// HasTestCaseFailure is used to check xml failure for given xml suite and test case
func (report *XMLReport) HasTestCaseFailure(i, j int) bool {
	return report.xmlSuites[i].Cases[j].Failure != nil
}

// TestCaseFailure is used to create new LogMessage with failure message for given xml suite and test case
func (report *XMLReport) TestCaseFailure(i, j int) *LogMessage {
	xSuite := report.xmlSuites[i]
	suiteStart := parseTimeStamp(xSuite.TimeStamp)
	xCase := xSuite.Cases[j]
	d := secondsToDuration(xCase.Time)
	xCaseEnd := suiteStart.Add(d)

	return &LogMessage{
		Time:    xCaseEnd,
		Level:   LogLevelError,
		Message: xCase.Failure.Message,
	}
}

// TestCaseFailureDetails is used to create new LogMessage with failure details for given xml suite and test case
func (report *XMLReport) TestCaseFailureDetails(i, j int) *LogMessage {
	xSuite := report.xmlSuites[i]
	suiteStart := parseTimeStamp(xSuite.TimeStamp)
	xCase := xSuite.Cases[j]
	d := secondsToDuration(xCase.Time)
	xCaseEnd := suiteStart.Add(d)
	return &LogMessage{
		Time:    xCaseEnd,
		Level:   LogLevelInfo,
		Message: xCase.Failure.Details,
	}
}

// parseXMLReport is used for parsing xml report sorted by suite start time
func parseXMLReport(reportDir string) ([]xmlSuite, error) {

	if len(reportDir) == 0 {
		return nil, errors.New("report dir could not be empty")
	}
	files, err := ioutil.ReadDir(reportDir)
	if err != nil {
		return nil, err
	}

	n := len(files)
	xSuites := make([]xmlSuite, 0)

	for i := 0; i < n; i++ {
		f := files[i]
		if filepath.Ext(f.Name()) != ".xml" || f.IsDir() {
			log.Debugf("not report file '%s'", f.Name())
			continue
		}

		xmlFile, err := os.Open(filepath.Join(reportDir, f.Name()))
		defer xmlFile.Close()
		if err != nil {
			log.Error(err)
			continue
		}

		b, err := ioutil.ReadAll(xmlFile)
		if err != nil {
			log.Error(err)
			continue
		}

		var xSuite xmlSuite
		err = xml.Unmarshal(b, &xSuite)
		if err != nil {
			log.Error(err)
			continue
		}

		xSuites = append(xSuites, xSuite)
	}

	// sort by start time
	sort.Slice(xSuites, func(i, j int) bool {
		t1 := parseTimeStamp(xSuites[i].TimeStamp)
		t2 := parseTimeStamp(xSuites[j].TimeStamp)
		return t1.Before(t2)
	})

	return xSuites, nil
}
