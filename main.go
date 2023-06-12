package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	tele "gopkg.in/telebot.v3"
)

type ChatUser struct {
	ChatID   int64 `gorm:"primaryKey"`
	UserID   int64 `gorm:"primaryKey"`
	Username string
}

var DB *gorm.DB

func ConnectDB() {
	var err error

	err = os.MkdirAll(filepath.Join(".", "data"), os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}

	DB, err = gorm.Open(sqlite.Open(filepath.Join(".", "data", "db.sqlite3")), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	err = DB.AutoMigrate(&ChatUser{})
	if err != nil {
		log.Fatal(err)
	}
}

func InitBot() *tele.Bot {
	b, err := tele.NewBot(tele.Settings{
		Token:  os.Getenv("TELEGRAM_TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}
	return b
}

func logger(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		log.Println("user", c.Sender().ID, "sent", c.Message().Text)
		return next(c)
	}
}

func handleStart(c tele.Context) error {
	return c.Send("Hey! I can help notify everyone 📢 in the group when someone needs them. " +
		"Everyone who wishes to receive mentions needs to /in to opt-in. " +
		"All opted-in users can then be mentioned using /all")
}

func handleIn(c tele.Context) error {
	username := extractUsername(c.Sender())
	DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chat_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username"}),
	}).Create(&ChatUser{ChatID: c.Message().Chat.ID, UserID: c.Sender().ID, Username: username})
	return c.Send("Thanks for opting in " + username)
}

func extractUsername(m *tele.User) string {
	if len(m.Username) > 0 {
		return m.Username
	}
	if len(m.FirstName) > 0 {
		return m.FirstName
	}
	return "anonymous"
}

func handleOut(c tele.Context) error {
	DB.Where("chat_id = ? and user_id = ?", c.Chat().ID, c.Sender().ID).Delete(&ChatUser{})
	msg := fmt.Sprintf("You've been opted out %v", extractUsername(c.Sender()))
	return c.Send(msg)
}

func handleAll(c tele.Context) error {
	var users []ChatUser
	DB.Find(&users, ChatUser{ChatID: c.Chat().ID})

	if len(users) == 0 {
		return c.Send("There are no users. To opt in type /in command")
	}

	var mentions []string
	for _, chatUser := range users {
		croppedUsername := string([]rune(chatUser.Username)[:10])
		mentions = append(mentions, fmt.Sprintf(`<a href="tg://user?id=%v">%v</a>`,
			chatUser.UserID, html.EscapeString(croppedUsername)))
	}

	const chunkSize = 4
	for i := 0; i < len(mentions); i += chunkSize {
		end := i + chunkSize
		if end > len(mentions) {
			end = len(mentions)
		}

		if err := c.Send(strings.Join(mentions[i:end], " "), tele.ModeHTML); err != nil {
			return err
		}
	}
	return nil
}

func handleStats(c tele.Context) error {
	var userCount int64
	var chatCount int64
	var groupCount int64

	DB.Model(&ChatUser{}).Distinct("user_id").Count(&userCount)
	DB.Model(&ChatUser{}).Distinct("chat_id").Count(&chatCount)
	DB.Model(&ChatUser{}).Select("count(*)").Group("chat_id").Having("count(*) > 1").Count(&groupCount)

	msg := fmt.Sprintf("`Users:  %6d\nChats:  %6d\nGroups: %6d`", userCount, chatCount, groupCount)
	return c.Send(msg, tele.ModeMarkdownV2)
}

func handleUserLeft(c tele.Context) error {
	u := ChatUser{UserID: c.Callback().Message.UserLeft.ID, ChatID: c.Chat().ID}
	log.Printf("user %d left chat %d", u.UserID, u.ChatID)
	DB.Where(&u).Delete(&ChatUser{})
	return nil
}

func main() {
	ConnectDB()
	b := InitBot()
	b.Use(logger)
	b.Handle("/start", handleStart)
	b.Handle("/in", handleIn)
	b.Handle("/out", handleOut)
	b.Handle("/all", handleAll)
	b.Handle("/stats", handleStats)
	b.Handle(tele.OnUserLeft, handleUserLeft)
	b.Start()
}
