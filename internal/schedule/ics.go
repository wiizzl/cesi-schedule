package schedule

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var icsEscaper = strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, "\n", `\n`, "\r", "")

func esc(s string) string { return icsEscaper.Replace(s) }

func fold(line string) string {
	if len(line) <= 75 {
		return line
	}

	var b strings.Builder

	start, limit := 0, 75
	for start < len(line) {
		end := start + limit
		if end > len(line) {
			end = len(line)
		}

		for end < len(line) && line[end]&0xc0 == 0x80 {
			end--
		}

		if start > 0 {
			b.WriteString("\r\n ")
		}

		b.WriteString(line[start:end])
		start, limit = end, 74
	}
	return b.String()
}

var reOffsetNoMin = regexp.MustCompile(`([+-]\d{2})$`)

func parseTime(s string) (time.Time, bool) {
	s = reOffsetNoMin.ReplaceAllString(s, "$1:00")
	t, err := time.Parse(time.RFC3339, s)

	return t, err == nil
}

func utc(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func dateOnly(t time.Time) string {
	return t.Format("20060102")
}

func toICS(seances []seance) string {
	stamp := utc(time.Now())
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//cesi-schedule//FR",
		"CALSCALE:GREGORIAN", "METHOD:PUBLISH", "X-WR-CALNAME:CESI Schedule",
	}

	for _, s := range seances {
		start, ok := parseTime(s.Start)
		if !ok {
			continue
		}

		end := start
		if t, ok := parseTime(s.End); ok {
			end = t
		}

		summary := firstNonEmpty(s.Matiere, s.Title, s.Theme, "Cours")

		var salles, profs, groupes []string
		for _, x := range s.Salles {
			if x.NomSalle != "" {
				salles = append(salles, x.NomSalle)
			}
		}

		for _, i := range s.Intervenants {
			if n := strings.TrimSpace(i.Prenom + " " + i.Nom); n != "" {
				profs = append(profs, n)
			}
		}

		for _, p := range s.Participants {
			if p.LibelleGroupe != "" {
				groupes = append(groupes, p.LibelleGroupe)
			}
		}

		var desc []string
		if s.Theme != "" && s.Theme != summary {
			desc = append(desc, s.Theme)
		}

		if len(profs) > 0 {
			desc = append(desc, "Intervenant(s) : "+strings.Join(profs, ", "))
		}

		if len(groupes) > 0 {
			desc = append(desc, "Groupe(s) : "+strings.Join(groupes, ", "))
		}

		uid := s.Code
		if uid == "" {
			uid = fmt.Sprintf("%d-%d", start.Unix(), end.Unix())
		}

		lines = append(lines, "BEGIN:VEVENT", "UID:"+esc(uid)+"@ent.cesi.fr", "DTSTAMP:"+stamp)
		if s.AllDay {
			if dateOnly(end) <= dateOnly(start) {
				end = start.AddDate(0, 0, 1)
			}
			lines = append(lines, "DTSTART;VALUE=DATE:"+dateOnly(start), "DTEND;VALUE=DATE:"+dateOnly(end))
		} else {
			lines = append(lines, "DTSTART:"+utc(start), "DTEND:"+utc(end))
		}

		lines = append(lines, "SUMMARY:"+esc(summary))
		if len(salles) > 0 {
			lines = append(lines, "LOCATION:"+esc(strings.Join(salles, ", ")))
		}
		if len(desc) > 0 {
			lines = append(lines, "DESCRIPTION:"+esc(strings.Join(desc, "\n")))
		}
		lines = append(lines, "END:VEVENT")
	}

	lines = append(lines, "END:VCALENDAR")

	for i, l := range lines {
		lines[i] = fold(l)
	}

	return strings.Join(lines, "\r\n") + "\r\n"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}
