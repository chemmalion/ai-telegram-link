package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "time"

    bolt "github.com/boltdb/bolt"
)

var db *bolt.DB

const (
    bucketProjects = "projects"
    bucketMapping  = "mapping" // key: chatID:topicID, value: projectName
)

func initStorage(path string) error {
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
        return nil
    })
}

func saveProject(name, apiKeyEnc string) error {
    return db.Update(func(tx *bolt.Tx) error {
        b := tx.Bucket([]byte(bucketProjects))
        return b.Put([]byte(name), []byte(apiKeyEnc))
    })
}

func loadProject(name string) (string, error) {
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

func mapTopic(chatID int64, topicID int, project string) error {
    key := fmt.Sprintf("%d:%d", chatID, topicID)
    return db.Update(func(tx *bolt.Tx) error {
        b := tx.Bucket([]byte(bucketMapping))
        return b.Put([]byte(key), []byte(project))
    })
}

func unmapTopic(chatID int64, topicID int) error {
    key := fmt.Sprintf("%d:%d", chatID, topicID)
    return db.Update(func(tx *bolt.Tx) error {
        b := tx.Bucket([]byte(bucketMapping))
        return b.Delete([]byte(key))
    })
}

func getMappedProject(chatID int64, topicID int) (string, error) {
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

func listProjects() ([]string, error) {
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
