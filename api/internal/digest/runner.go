package digest

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/james-see/everydayisaholiday/api/internal/config"
	"github.com/james-see/everydayisaholiday/api/internal/mail"
)

type Runner struct {
	DB       *sql.DB
	Cfg      config.Config
	Mailer   mail.Sender
	Holidays *Store
}

// RunOnce sends digests to subscribers whose local time is at DigestHour
// and who have not already been sent for that local calendar date.
func (r *Runner) RunOnce(now time.Time) (sent int, err error) {
	rows, err := r.DB.Query(`
SELECT p.user_id, u.email, p.timezone, p.categories, p.unsub_token, p.last_sent_local_date
FROM email_prefs p
JOIN users u ON u.id = p.user_id
WHERE p.enabled = 1
  AND u.email_verified_at IS NOT NULL
  AND u.email_verified_at != ''
`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct {
		userID   int64
		email    string
		tz       string
		catsJSON string
		unsub    string
		lastSent sql.NullString
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.userID, &x.email, &x.tz, &x.catsJSON, &x.unsub, &x.lastSent); err != nil {
			return sent, err
		}
		list = append(list, x)
	}
	if err := rows.Err(); err != nil {
		return sent, err
	}

	for _, x := range list {
		loc, err := time.LoadLocation(x.tz)
		if err != nil {
			log.Printf("digest: bad timezone user=%d tz=%q: %v", x.userID, x.tz, err)
			continue
		}
		local := now.In(loc)
		if local.Hour() != r.Cfg.DigestHour {
			continue
		}
		localDate := local.Format("2006-01-02")
		if x.lastSent.Valid && x.lastSent.String == localDate {
			continue
		}
		var cats []string
		_ = json.Unmarshal([]byte(x.catsJSON), &cats)
		holidays := r.Holidays.ForDate(int(local.Month()), local.Day(), cats)
		subject, text, html := FormatDigest(local, holidays, r.Cfg.PublicBaseURL)
		unsubURL := r.Cfg.PublicBaseURL + "/unsubscribe?token=" + x.unsub
		text += "\nUnsubscribe: " + unsubURL + "\n"
		html = html + `<p style="font-size:12px;color:#8a8a9a"><a href="` + unsubURL + `" style="color:#8a8a9a">Unsubscribe</a></p></body></html>`

		err = r.Mailer.SendMessage(mail.Message{
			To:      x.email,
			Subject: subject,
			Text:    text,
			HTML:    html,
			Headers: map[string]string{
				"List-Unsubscribe":      "<" + unsubURL + ">",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			},
		})
		if err != nil {
			log.Printf("digest: send user=%d: %v", x.userID, err)
			continue
		}
		_, err = r.DB.Exec(`UPDATE email_prefs SET last_sent_local_date = ?, updated_at = ? WHERE user_id = ?`,
			localDate, time.Now().UTC().Format(time.RFC3339), x.userID)
		if err != nil {
			log.Printf("digest: mark sent user=%d: %v", x.userID, err)
			continue
		}
		sent++
	}
	return sent, nil
}

func (r *Runner) StartLoop() {
	go func() {
		time.Sleep(3 * time.Second)
		for {
			n, err := r.RunOnce(time.Now().UTC())
			if err != nil {
				log.Printf("digest: run error: %v", err)
			} else if n > 0 {
				log.Printf("digest: sent %d", n)
			}
			time.Sleep(r.Cfg.DigestInterval)
		}
	}()
}
