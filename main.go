package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/spf13/pflag"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

const CommandName = "kubectl-cronjob-timetable"

const ExitCodeErr = 1

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitCodeErr)
	}
}

func run(stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	var (
		sinceFlag           string
		untilFlag           string
		displayLocationFlag string
		allNamespacesFlag   bool
		noHeadersFlag       bool
		versionFlag         bool
	)

	fsets := pflag.NewFlagSet(CommandName, pflag.ContinueOnError)
	fsets.SetOutput(stderr)
	fsets.StringVarP(&sinceFlag, "since", "", "", "Absolute start time of the period to get the timetable.")
	fsets.StringVarP(&untilFlag, "until", "", "", "Absolute end time of the period to get the timetable.")
	fsets.StringVarP(&displayLocationFlag, "display-location", "", time.Local.String(), "Specify the timezone of the timetable time to be displayed. e.g. 'UTC', 'Asia/Tokyo', etc...")
	fsets.BoolVarP(&allNamespacesFlag, "all-namespaces", "A", false, "If present, list timetables across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	fsets.BoolVarP(&noHeadersFlag, "no-headers", "", false, "If present, don't print headers.")
	fsets.BoolVarP(&versionFlag, "version", "V", false, "Prints version information.")
	cfgFlags := genericclioptions.NewConfigFlags(true)
	cfgFlags.AddFlags(fsets)

	fsets.Usage = func() {
		fmt.Fprintf(stderr, "Usage of %s:\n", CommandName)
		fmt.Fprint(stderr, fsets.FlagUsages())
	}

	if err := fsets.Parse(args[1:]); err != nil {
		return err
	}

	if versionFlag {
		fmt.Fprintf(stdout, "%s %s (rev:%s)\n", CommandName, Version, Revision)
		return nil
	}

	// Set the timetable period
	// -------------------------------

	var (
		since time.Time
		until time.Time
	)

	// Specify the timezone of the timetable time to be displayed
	location, err := time.LoadLocation(displayLocationFlag)
	if err != nil {
		return fmt.Errorf("Unknown location '%s': %w", displayLocationFlag, err)
	}

	// Set the start time of the period in absolute time.
	if sinceFlag != "" {
		since, err = parseAbsoluteTime(sinceFlag, location)
		if err != nil {
			return fmt.Errorf("Failed to parse since time: %w", err)
		}
	}

	// Set the end time of the period in absolute time.
	if untilFlag != "" {
		until, err = parseAbsoluteTime(untilFlag, location)
		if err != nil {
			return fmt.Errorf("Failed to parse until time: %w", err)
		}
	}

	if since.After(until) {
		return fmt.Errorf("Since time is newer than until time")
	}

	switch {
	case since.IsZero() && until.IsZero():
		// If neither since time nor until time is specified, the period will be one hour after the current time.
		since = time.Now()
		until = since.Add(1 * time.Hour)
	case since.IsZero() && !until.IsZero():
		// If only since time is not specified, the period is from the current time to until time.
		since = time.Now()
	case !since.IsZero() && until.IsZero():
		// If only until time is not specified, the period is from the current time to since time.
		until = time.Now()
	}

	if diff := until.Day() - since.Day(); diff > 365*5 {
		return fmt.Errorf("Does not support differences over 5 years. %s - %s = %d days", until, since, diff)
	}

	// Get CronJobs
	// -------------------------------

	cfg, err := cfgFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("Could not get kubernetes REST client configuration: %w", err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("Could not get kubernetes client: %w", err)
	}

	defaultNamespace, _, err := cfgFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("Could not get default namespace from kubernetes client config: %w", err)
	}

	targetNamespace := defaultNamespace
	if allNamespacesFlag {
		targetNamespace = ""
	} else if *cfgFlags.Namespace != "" {
		targetNamespace = *cfgFlags.Namespace
	}

	cronjobList, err := client.BatchV1beta1().CronJobs(targetNamespace).List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("Failed to get CronJob list in '%s': %w", defaultNamespace, err)
	}

	// If there is no CronJob in the specified Namespace, return early.
	if len(cronjobList.Items) == 0 {
		if allNamespacesFlag {
			fmt.Fprintln(stdout, "No CronJobs found in all namespaces.")
		} else {
			fmt.Fprintf(stdout, "No CronJobs found in %s namespace.\n", targetNamespace)
		}
		return nil
	}

	// Print timetable
	// -------------------------------

	// Generate Timetable from CronJobs.
	tt, err := generateTimetable(cronjobList.Items, since, until)
	if err != nil {
		return fmt.Errorf("Failed generate timetable from CronJobs: %w", err)
	}

	// If there is no CronJob to execute during the specified period, return early
	if len(tt) == 0 {
		fmt.Fprintln(stdout, "No CronJob to execute during the specified period")
		return nil
	}

	// Sort key in timetable
	keyTimes := make([]time.Time, len(tt))
	for t := range tt {
		keyTimes = append(keyTimes, t)
	}
	sort.Slice(keyTimes, func(i, j int) bool {
		return keyTimes[i].Before(keyTimes[j])
	})

	// Print timetable
	w := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	if !noHeadersFlag {
		fmt.Fprintln(w, "TIME\tNAMESPACE\tNAME\tSUSPEND\tSCHEDULE")
	}
	for _, keyTime := range keyTimes {
		for _, ttc := range tt[keyTime] {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", keyTime.String(), ttc.namespace, ttc.name, strconv.FormatBool(ttc.suspend), ttc.schedule)
		}
	}
	w.Flush()

	return nil
}

type timetable map[time.Time][]timetableColumns

type timetableColumns struct {
	namespace string
	name      string
	schedule  string
	suspend   bool
}

func generateTimetable(cronjobs []batchv1beta1.CronJob, since, until time.Time) (timetable, error) {
	tt := timetable{}

	for _, cronjob := range cronjobs {
		sched, err := cron.ParseStandard(cronjob.Spec.Schedule)
		if err != nil {
			return nil, fmt.Errorf("Could not parse schedule spec '%s' of CronJob '%s/%s': %w", cronjob.Spec.Schedule, cronjob.Namespace, cronjob.Name, err)
		}
		for _, schedTime := range scheduleTimeList(sched, since, until) {
			tt[schedTime] = append(tt[schedTime], timetableColumns{
				namespace: cronjob.Namespace,
				name:      cronjob.Name,
				schedule:  cronjob.Spec.Schedule,
				suspend:   *cronjob.Spec.Suspend,
			})
		}
	}

	return tt, nil
}

func parseAbsoluteTime(value string, destLocation *time.Location) (time.Time, error) {
	var layouts = []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02T15",
		"2006-01-02",
		"2006-01",
		"2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.In(destLocation), nil
		}
	}
	return time.Time{}, fmt.Errorf("Could not parse time value '%s'", value)
}

func scheduleTimeList(sched cron.Schedule, since, until time.Time) []time.Time {
	ret := []time.Time{}

	// If the start time is included in the schedule, include it in the return value
	sinceMinusOneSec := since.Add(time.Duration(-1) * time.Second)
	if sched.Next(sinceMinusOneSec).Equal(since) {
		ret = append(ret, since)
	}

	t := since
	for {
		t = sched.Next(t)
		if t.After(until) {
			break
		}
		ret = append(ret, t)
	}

	return ret
}
