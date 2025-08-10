package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	bolt "github.com/boltdb/bolt"
)

var db *bolt.DB

const (
	bucketProjects      = "projects"
	bucketMapping       = "mapping"        // key: chatID:topicID, value: projectName
	bucketModels        = "models"         // key: projectName, value: model
	bucketRules         = "rules"          // key: projectName, value: custom instruction
	bucketHistoryLimits = "history_limits" // key: projectName, value: limit
	bucketHistory       = "history"        // parent bucket for per-project history
)

// Init opens the database file and creates buckets if needed.
func Init(path string) error {
	var err error
	db, err = bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return err
	}
	return db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketProjects)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketMapping)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketModels)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketRules)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketHistoryLimits)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketHistory)); err != nil {
			return err
		}
		return nil
	})
}

// SaveProject registers a project name without any associated API key.
func SaveProject(name string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketProjects))
		return b.Put([]byte(name), []byte{})
	})
}

// ProjectExists checks if a project is present in the database.
func ProjectExists(name string) (bool, error) {
	var exists bool
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketProjects))
		v := b.Get([]byte(name))
		if v != nil {
			exists = true
		}
		return nil
	})
	return exists, err
}

// SaveProjectModel stores the selected model for the given project.
func SaveProjectModel(name, model string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketModels))
		return b.Put([]byte(name), []byte(model))
	})
}

// LoadProjectModel returns the stored model for the project.
func LoadProjectModel(name string) (string, error) {
	var val []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketModels))
		v := b.Get([]byte(name))
		if v == nil {
			return errors.New("model not found")
		}
		val = append([]byte(nil), v...)
		return nil
	})
	return string(val), err
}

// SaveProjectInstruction stores the custom instruction for the project.
func SaveProjectInstruction(name, instr string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRules))
		return b.Put([]byte(name), []byte(instr))
	})
}

// LoadProjectInstruction returns the stored instruction for the project.
func LoadProjectInstruction(name string) (string, error) {
	var val []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRules))
		v := b.Get([]byte(name))
		if v == nil {
			return errors.New("rule not found")
		}
		val = append([]byte(nil), v...)
		return nil
	})
	return string(val), err
}

// MapTopic links a chat topic to a project.
func MapTopic(chatID int64, topicID int, project string) error {
	key := fmt.Sprintf("%d:%d", chatID, topicID)
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMapping))
		return b.Put([]byte(key), []byte(project))
	})
}

// UnmapTopic removes the association between a chat topic and a project.
func UnmapTopic(chatID int64, topicID int) error {
	key := fmt.Sprintf("%d:%d", chatID, topicID)
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMapping))
		return b.Delete([]byte(key))
	})
}

// GetMappedProject returns the project associated with the chat topic.
func GetMappedProject(chatID int64, topicID int) (string, error) {
	var proj []byte
	key := fmt.Sprintf("%d:%d", chatID, topicID)
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMapping))
		v := b.Get([]byte(key))
		if v == nil {
			return errors.New("no project mapped")
		}
		proj = append([]byte(nil), v...)
		return nil
	})
	return string(proj), err
}

// ListProjects returns all stored project names.
func ListProjects() ([]string, error) {
	var names []string
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketProjects))
		return b.ForEach(func(k, _ []byte) error {
			names = append(names, string(k))
			return nil
		})
	})
	return names, err
}

// HistoryMessage represents a stored message in project history.
type HistoryMessage struct {
	Role    string `json:"role"`
	WhoID   int64  `json:"who_id"`
	WhoName string `json:"who_name"`
	When    int64  `json:"when"`
	Content string `json:"content"`
}

// SaveHistoryLimit sets the history limit for a project.
func SaveHistoryLimit(project string, limit int) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketHistoryLimits))
		return b.Put([]byte(project), []byte(strconv.Itoa(limit)))
	})
}

// LoadHistoryLimit retrieves the history limit for a project. Default is 0.
func LoadHistoryLimit(project string) (int, error) {
	var limit int
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketHistoryLimits))
		v := b.Get([]byte(project))
		if v == nil {
			limit = 0
			return nil
		}
		i, err := strconv.Atoi(string(v))
		if err != nil {
			return err
		}
		limit = i
		return nil
	})
	return limit, err
}

// AddHistoryMessage stores a message for the given project.
func AddHistoryMessage(project string, msg HistoryMessage) error {
	return db.Update(func(tx *bolt.Tx) error {
		hb := tx.Bucket([]byte(bucketHistory))
		pb, err := hb.CreateBucketIfNotExists([]byte(project))
		if err != nil {
			return err
		}
		id, _ := pb.NextSequence()
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return pb.Put(key, data)
	})
}

// LoadProjectHistory returns all stored history messages for a project.
func LoadProjectHistory(project string) ([]HistoryMessage, error) {
	var items []HistoryMessage
	err := db.View(func(tx *bolt.Tx) error {
		hb := tx.Bucket([]byte(bucketHistory))
		pb := hb.Bucket([]byte(project))
		if pb == nil {
			return nil
		}
		return pb.ForEach(func(_, v []byte) error {
			var m HistoryMessage
			if err := json.Unmarshal(v, &m); err != nil {
				return err
			}
			items = append(items, m)
			return nil
		})
	})
	return items, err
}

// CountProjectHistory returns the number of stored messages for a project.
func CountProjectHistory(project string) (int, error) {
	var count int
	err := db.View(func(tx *bolt.Tx) error {
		hb := tx.Bucket([]byte(bucketHistory))
		pb := hb.Bucket([]byte(project))
		if pb == nil {
			count = 0
			return nil
		}
		stats := pb.Stats()
		count = stats.KeyN
		return nil
	})
	return count, err
}

// TrimProjectHistory ensures the stored messages do not exceed the limit.
func TrimProjectHistory(project string, limit int) error {
	if limit <= 0 {
		return nil
	}
	return db.Update(func(tx *bolt.Tx) error {
		hb := tx.Bucket([]byte(bucketHistory))
		pb := hb.Bucket([]byte(project))
		if pb == nil {
			return nil
		}
		stats := pb.Stats()
		excess := stats.KeyN - limit
		if excess <= 0 {
			return nil
		}
		c := pb.Cursor()
		for i := 0; i < excess; i++ {
			k, _ := c.First()
			if k == nil {
				break
			}
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClearProjectHistory deletes all stored messages for a project and returns the number removed.
func ClearProjectHistory(project string) (int, error) {
	count, err := CountProjectHistory(project)
	if err != nil {
		return 0, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		hb := tx.Bucket([]byte(bucketHistory))
		return hb.DeleteBucket([]byte(project))
	})
	return count, err
}
