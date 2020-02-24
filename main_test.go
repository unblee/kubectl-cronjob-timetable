package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getCronJob(namespace, name, schedule string, suspend bool) batchv1beta1.CronJob {
	return batchv1beta1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: batchv1beta1.CronJobSpec{
			Schedule: schedule,
			Suspend:  &suspend,
		},
	}
}

func getLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		msg := fmt.Sprintf("failed to get location from name '%s': %s", name, err)
		panic(msg)
	}
	return loc
}

func getTime(value string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02T15:04:05", value, loc)
	if err != nil {
		msg := fmt.Sprintf("failed to get time from value '%s': %s", value, err)
		panic(msg)
	}
	return t
}

func getSchedule(spec string) cron.Schedule {
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		msg := fmt.Sprintf("failed to get schedule from spec '%s': %s", spec, err)
		panic(msg)
	}
	return sched
}

func Test_generateTimetable(t *testing.T) {
	utc := getLocation("UTC")
	type args struct {
		cronjobs []batchv1beta1.CronJob
		since    time.Time
		until    time.Time
	}
	tests := []struct {
		name    string
		args    args
		want    timetable
		wantErr bool
	}{
		{
			name: "Simple case",
			args: args{
				cronjobs: []batchv1beta1.CronJob{
					getCronJob("foo", "bar", "55 * * * *", false),
					getCronJob("baz", "qux", "50/5 23,0 * * *", true),
				},
				since: getTime("2019-12-31T22:00:00", utc),
				until: getTime("2020-01-01T02:00:00", utc),
			},
			want: timetable{
				getTime("2019-12-31T22:55:00", utc): []timetableColumns{
					timetableColumns{namespace: "foo", name: "bar", schedule: "55 * * * *", suspend: false},
				},
				getTime("2019-12-31T23:50:00", utc): []timetableColumns{
					timetableColumns{namespace: "baz", name: "qux", schedule: "50/5 23,0 * * *", suspend: true},
				},
				getTime("2019-12-31T23:55:00", utc): []timetableColumns{
					timetableColumns{namespace: "foo", name: "bar", schedule: "55 * * * *", suspend: false},
					timetableColumns{namespace: "baz", name: "qux", schedule: "50/5 23,0 * * *", suspend: true},
				},
				getTime("2020-01-01T00:50:00", utc): []timetableColumns{
					timetableColumns{namespace: "baz", name: "qux", schedule: "50/5 23,0 * * *", suspend: true},
				},
				getTime("2020-01-01T00:55:00", utc): []timetableColumns{
					timetableColumns{namespace: "foo", name: "bar", schedule: "55 * * * *", suspend: false},
					timetableColumns{namespace: "baz", name: "qux", schedule: "50/5 23,0 * * *", suspend: true},
				},
				getTime("2020-01-01T01:55:00", utc): []timetableColumns{
					timetableColumns{namespace: "foo", name: "bar", schedule: "55 * * * *", suspend: false},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateTimetable(tt.args.cronjobs, tt.args.since, tt.args.until)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateTimetable() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("generateTimetable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseAbsoluteTime(t *testing.T) {
	utc := getLocation("UTC")
	type args struct {
		value        string
		destLocation *time.Location
	}
	tests := []struct {
		name    string
		args    args
		want    time.Time
		wantErr bool
	}{
		{
			name: "yyyy-mm-ddThh:mm:ssZ",
			args: args{
				value:        "2020-01-01T00:00:00Z",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy-mm-ddThh:mm:ss±mm:ss",
			args: args{
				value:        "2020-01-01T00:00:00+00:00",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy-mm-ddThh:mm:ss",
			args: args{
				value:        "2020-01-01T00:00:00",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy-mm-ddThh:mm",
			args: args{
				value:        "2020-01-01T00:00",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy-mm-ddThh",
			args: args{
				value:        "2020-01-01T00",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy-mm-dd",
			args: args{
				value:        "2020-01-01",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy-mm",
			args: args{
				value:        "2020-01",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "yyyy",
			args: args{
				value:        "2020",
				destLocation: utc,
			},
			want:    getTime("2020-01-01T00:00:00", utc),
			wantErr: false,
		},
		{
			name: "Different location",
			args: args{
				value:        "2020-01-01T00:00:00Z",
				destLocation: getLocation("Asia/Tokyo"),
			},
			want:    getTime("2020-01-01T09:00:00", getLocation("Asia/Tokyo")),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAbsoluteTime(tt.args.value, tt.args.destLocation)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAbsoluteTime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseAbsoluteTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_scheduleTimeList(t *testing.T) {
	utc := getLocation("UTC")
	type args struct {
		sched cron.Schedule
		since time.Time
		until time.Time
	}
	tests := []struct {
		name string
		args args
		want []time.Time
	}{
		{
			name: "Simple case",
			args: args{
				sched: getSchedule("0 * * * *"),
				since: getTime("2020-01-01T00:00:00", utc),
				until: getTime("2020-01-01T02:00:00", utc),
			},
			want: []time.Time{
				getTime("2020-01-01T00:00:00", utc),
				getTime("2020-01-01T01:00:00", utc),
				getTime("2020-01-01T02:00:00", utc),
			},
		},
		{
			name: "List case",
			args: args{
				sched: getSchedule("0,30 15,20 * * *"),
				since: getTime("2020-01-01T00:00:00", utc),
				until: getTime("2020-01-01T23:59:59", utc),
			},
			want: []time.Time{
				getTime("2020-01-01T15:00:00", utc),
				getTime("2020-01-01T15:30:00", utc),
				getTime("2020-01-01T20:00:00", utc),
				getTime("2020-01-01T20:30:00", utc),
			},
		},
		{
			name: "Step case",
			args: args{
				sched: getSchedule("*/5 * * * *"),
				since: getTime("2019-12-31T23:50:01", utc),
				until: getTime("2020-01-01T00:09:59", utc),
			},
			want: []time.Time{
				getTime("2019-12-31T23:55:00", utc),
				getTime("2020-01-01T00:00:00", utc),
				getTime("2020-01-01T00:05:00", utc),
			},
		},
		{
			name: "Range case",
			args: args{
				sched: getSchedule("0 15-16 29-30 * *"),
				since: getTime("2020-01-01T00:00:00", utc),
				until: getTime("2020-01-31T23:59:59", utc),
			},
			want: []time.Time{
				getTime("2020-01-29T15:00:00", utc),
				getTime("2020-01-29T16:00:00", utc),
				getTime("2020-01-30T15:00:00", utc),
				getTime("2020-01-30T16:00:00", utc),
			},
		},
		{
			name: "Step and min only range case",
			args: args{
				sched: getSchedule("50/5 * * * *"),
				since: getTime("2020-01-01T00:00:00", utc),
				until: getTime("2020-01-01T01:00:00", utc),
			},
			want: []time.Time{
				getTime("2020-01-01T00:50:00", utc),
				getTime("2020-01-01T00:55:00", utc),
			},
		},
		{
			name: "Step and range case",
			args: args{
				sched: getSchedule("50-59/5 * * * *"),
				since: getTime("2020-01-01T00:00:00", utc),
				until: getTime("2020-01-01T01:00:00", utc),
			},
			want: []time.Time{
				getTime("2020-01-01T00:50:00", utc),
				getTime("2020-01-01T00:55:00", utc),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scheduleTimeList(tt.args.sched, tt.args.since, tt.args.until); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scheduleTimeList() = %v, want %v", got, tt.want)
			}
		})
	}
}
