package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type ACTIONTYPE int

const (
	UPDATE_ONLY ACTIONTYPE = iota
	CREATE_UPDATE
	RESERVATIONS
)

type envelope map[string]interface{}

type application struct {
	waitLists map[string][]msg
	minidb    map[string]string
	lockdb    map[string]string
	msgCh     chan msg
	wg        *sync.WaitGroup
}

type msg struct {
	responseChan chan msgResp
	actionType   ACTIONTYPE
	key          string
	newVal       string
	lockId       string
	toRelease    bool
}

type msgResp struct {
	status int    `json:"-"`
	Value  string `json:"value,omitempty"`
	LockId string `json:"lock_id"`
}

func (app *application) startApplication() {
	for msg_ := range app.msgCh {
		switch msg_.actionType {
		case UPDATE_ONLY:
			_, ok := app.minidb[msg_.key]
			if !ok {
				msg_.responseChan <- msgResp{
					status: http.StatusNotFound,
				}
			} else {
				lockId, ok := app.lockdb[msg_.key]
				if ok && lockId == msg_.lockId {
					app.minidb[msg_.key] = msg_.newVal
					msg_.responseChan <- msgResp{
						status: http.StatusNoContent,
					}
					if msg_.toRelease {
						delete(app.lockdb, msg_.key)
						// assign the lock to others waiting in the queue
						newLockId := uuid.New().String()
						app.assignLockToWaitingClient(msg_.key, newLockId)
					}
				} else {
					msg_.responseChan <- msgResp{
						status: http.StatusUnauthorized,
					}
				}
			}
		case CREATE_UPDATE:
			_, keyExists := app.minidb[msg_.key]
			_, lockExists := app.lockdb[msg_.key]
			if !keyExists || (keyExists && !lockExists) {
				app.minidb[msg_.key] = msg_.newVal
				lockId := uuid.New().String()
				app.lockdb[msg_.key] = lockId
				msg_.responseChan <- msgResp{
					status: http.StatusOK,
					LockId: lockId,
				}
			} else {
				// add to the waiting list
				_, ok := app.waitLists[msg_.key]
				if !ok {
					app.waitLists[msg_.key] = make([]msg, 0)
				}
				app.waitLists[msg_.key] = append(app.waitLists[msg_.key], msg_)
			}

		case RESERVATIONS:
			val, keyExists := app.minidb[msg_.key]
			_, lockExists := app.lockdb[msg_.key]
			if !keyExists {
				msg_.responseChan <- msgResp{
					status: http.StatusNotFound,
				}
			} else {
				if !lockExists {
					lockId := uuid.New().String()
					app.lockdb[msg_.key] = lockId
					msg_.responseChan <- msgResp{
						status: http.StatusOK,
						Value:  val,
						LockId: lockId,
					}
				} else {
					// add to the waiting list
					_, ok := app.waitLists[msg_.key]
					if !ok {
						app.waitLists[msg_.key] = make([]msg, 0)
					}
					app.waitLists[msg_.key] = append(app.waitLists[msg_.key], msg_)
				}
			}

		}
	}
	app.wg.Done()
}

func (app *application) assignLockToWaitingClient(key, lockId string) {
	waitList, ok := app.waitLists[key]
	if ok && len(waitList) > 0 {
		cl := waitList[0]
		if cl.actionType == CREATE_UPDATE {
			app.minidb[key] = cl.newVal
			cl.responseChan <- msgResp{
				status: http.StatusOK,
				LockId: lockId,
			}
		} else if cl.actionType == RESERVATIONS {
			cl.responseChan <- msgResp{
				status: http.StatusOK,
				Value:  app.minidb[key],
				LockId: lockId,
			}
		}
		app.lockdb[key] = lockId
		app.waitLists[key] = waitList[1:]
	}
}

func writeJSON(w http.ResponseWriter, status int, data envelope, headers http.Header) error {
	js, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return err
	}

	js = append(js, '\n')

	for key, value := range headers {
		w.Header()[key] = value
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)

	return nil
}

func readJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	maxBytes := 1_848_576
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	err := dec.Decode(dst)
	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError
		var invalidUnmarshalError *json.InvalidUnmarshalError
		switch {
		case errors.As(err, &syntaxError):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxError.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")
		case errors.As(err, &unmarshalTypeError):
			if unmarshalTypeError.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", unmarshalTypeError.Field)
			}
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", unmarshalTypeError.Offset)
		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("body contains unknown key %s", fieldName)
		case err.Error() == "http: request body too large":
			return fmt.Errorf("body must not be larger than %d bytes", maxBytes)
		case errors.As(err, &invalidUnmarshalError):
			panic(err)
		default:
			return err
		}
	}
	err = dec.Decode(&struct{}{})
	if err != io.EOF {
		return errors.New("body must only contain a single json value")
	}
	return nil
}

func errorResponse(w http.ResponseWriter, status int, message string) {
	env := envelope{"error": message}
	err := writeJSON(w, status, env, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// should wait return value and lock_id
func (app *application) reservationsHandler(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		message := "empty key provided"
		errorResponse(w, http.StatusNotFound, message)
		return
	}

	ch := make(chan msgResp)
	app.msgCh <- msg{
		responseChan: ch,
		actionType:   RESERVATIONS,
		key:          key,
	}
	resp := <-ch
	if resp.status == http.StatusNotFound {
		errorResponse(w, resp.status, "")
	} else {
		writeJSON(w, resp.status, envelope{"value": resp.Value, "lock_id": resp.LockId}, nil)
	}
}

// should not wait
func (app *application) updateOnlyHandler(w http.ResponseWriter, r *http.Request) {
	releaseStr := r.URL.Query().Get("release")
	if releaseStr != "true" && releaseStr != "false" {
		message := "Invalid release value, must be 'true' or 'false'"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}

	var toRelease bool
	if releaseStr == "true" {
		toRelease = true
	} else {
		toRelease = false
	}

	key := chi.URLParam(r, "key")
	if key == "" {
		message := "empty key provided"
		errorResponse(w, http.StatusNotFound, message)
		return
	}
	lockId := chi.URLParam(r, "lock_id")
	if lockId == "" {
		message := "empty lock_id provided"
		errorResponse(w, http.StatusNotFound, message)
		return
	}

	var input struct {
		Value string `json:"value"`
	}
	err := readJSON(w, r, &input)
	if err != nil {
		message := err.Error()
		errorResponse(w, http.StatusBadRequest, message)
		return
	}

	ch := make(chan msgResp)
	app.msgCh <- msg{
		responseChan: ch,
		actionType:   UPDATE_ONLY,
		key:          key,
		lockId:       lockId,
		toRelease:    toRelease,
		newVal:       input.Value,
	}
	resp := <-ch
	if resp.status == http.StatusNoContent {
		writeJSON(w, resp.status, nil, nil)
	} else {
		errorResponse(w, resp.status, "")
	}
}

// should wait
func (app *application) createUpdateHandler(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		message := "empty key provided"
		errorResponse(w, http.StatusNotFound, message)
		return
	}

	var input struct {
		Value string `json:"value"`
	}
	err := readJSON(w, r, &input)
	if err != nil {
		message := err.Error()
		errorResponse(w, http.StatusBadRequest, message)
		return
	}

	ch := make(chan msgResp)
	app.msgCh <- msg{
		responseChan: ch,
		actionType:   CREATE_UPDATE,
		key:          key,
		newVal:       input.Value,
	}
	resp := <-ch

	writeJSON(w, resp.status, envelope{"lock_id": resp.LockId}, nil)
}

func main() {
	msgChannelSize := 512
	msgChan := make(chan msg, msgChannelSize)

	wg := &sync.WaitGroup{}

	app := application{
		minidb:    make(map[string]string),
		lockdb:    make(map[string]string),
		waitLists: make(map[string][]msg),
		msgCh:     msgChan,
		wg:        wg,
	}
	wg.Add(1)
	go app.startApplication()

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Post("/reservations/{key}", app.reservationsHandler)
	router.Route("/values", func(r1 chi.Router) {
		r1.Put("/{key}", app.createUpdateHandler)
		r1.Post("/{key}/{lock_id}", app.updateOnlyHandler)
	})

	server := &http.Server{Addr: "0.0.0.0:3333", Handler: router}

	serverCtx, serverStopCtx := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sig
		close(msgChan)
		wg.Wait()
		shutdownCtx, cf := context.WithTimeout(serverCtx, 30*time.Second)

		go func() {
			<-shutdownCtx.Done()
			cf()
			if shutdownCtx.Err() == context.DeadlineExceeded {
				log.Fatal("graceful shutdown timed out.. forcing exit.")
			}
		}()

		err := server.Shutdown(shutdownCtx)
		if err != nil {
			log.Fatal(err)
		}
		serverStopCtx()
	}()
	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-serverCtx.Done()
}
