package analysis

import (
	"fmt"
	"time"
)

// FormatTimestamp is forma2's getFormattedDateTimeString: Intl.DateTimeFormat("en-CA") with a 24-hour clock and the short zone name, the comma removed ("2026-01-01 20:00:00 UTC", "2026-01-02 04:00:00 GMT+8"), in the organization's display time zone. A zone that does not load is UTC, as forma2 fell back on a RangeError.
func FormatTimestamp(t time.Time, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		zone, loc = "UTC", time.UTC
	}
	local := t.In(loc)
	_, offset := local.Zone()
	return local.Format("2006-01-02 15:04:05") + " " + shortZoneName(zone, offset)
}

// shortZoneName is the zone's short name at the offset as en-CA prints it: a name CLDR has for it, else "GMT" with the offset in hours and, when needed, minutes.
func shortZoneName(zone string, offset int) string {
	if name, ok := zoneNames[zone][offset]; ok {
		return name
	}
	if offset == 0 {
		return "GMT"
	}
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	hours, minutes := offset/3600, offset%3600/60
	if minutes == 0 {
		return fmt.Sprintf("GMT%s%d", sign, hours)
	}
	return fmt.Sprintf("GMT%s%d:%02d", sign, hours, minutes)
}
