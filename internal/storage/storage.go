package storage

import (
	"errors"
	"fmt"
	"time"

	bolt "github.com/boltdb/bolt"
)

var db *bolt.DB

const (
	bucketProjects = "projects"
	bucketMapping  = "mapping" // key: chatID:topicID, value: projectName
	bucketModels   = "models"  // key: projectName, value: model
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
		return nil
	})
}

// SaveProject stores the encrypted API key under the given project name.
func SaveProject(name, apiKeyEnc string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketProjects))
		return b.Put([]byte(name), []byte(apiKeyEnc))
	})
}

// LoadProject returns the encrypted API key for the project.
func LoadProject(name string) (string, error) {
	var val []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketProjects))
		v := b.Get([]byte(name))
		if v == nil {
			return errors.New("project not found")
		}
		val = append([]byte(nil), v...)
		return nil
	})
	return string(val), err
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
