package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type envelope map[string]interface{}

type application struct {
	totalHeights int
	totalWidths  int
	imagesCount  int
	msgCh        chan msg
	wg           *sync.WaitGroup
}

type stats struct {
	AverageWidth  float32 `json:"average_width"`
	AverageHeight float32 `json:"average_height"`
	Images        int     `json:"num_images"`
}

type msg struct {
	responseChan  chan stats
	width, height int
}

func (app *application) startApplication() {
	for msg_ := range app.msgCh {
		if msg_.responseChan == nil {
			app.totalHeights += msg_.height
			app.totalWidths += msg_.width
			app.imagesCount += 1
		} else {
			msg_.responseChan <- stats{
				AverageWidth:  float32(app.totalWidths) / float32(app.imagesCount),
				AverageHeight: float32(app.totalHeights) / float32(app.imagesCount),
				Images:        app.imagesCount,
			}
		}
	}
	app.wg.Done()
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

func writeImage(w http.ResponseWriter, img *image.Image, jpgOrPng string) error {
	buffer := new(bytes.Buffer)
	if jpgOrPng == "jpg" {
		if err := jpeg.Encode(buffer, *img, nil); err != nil {
			return err
		}
	}

	if jpgOrPng == "png" {
		if err := png.Encode(buffer, *img); err != nil {
			return err
		}
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(buffer.Bytes())))
	if _, err := w.Write(buffer.Bytes()); err != nil {
		log.Println("unable to write image.")
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

func (app *application) pngHandler(w http.ResponseWriter, r *http.Request) {
	width_px := chi.URLParam(r, "width_px")
	height_px := chi.URLParam(r, "height_px")
	if width_px == "" || height_px == "" {
		message := "width_px or height_px not specified"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}
	width, err := strconv.Atoi(width_px)
	if err != nil {
		message := "width of an image must be a non negative integer"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}
	height, err := strconv.Atoi(height_px)
	if err != nil {
		message := "height of an image must be a non negative integer"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}

	app.msgCh <- msg{
		height: height,
		width:  width,
	}

	m := image.NewRGBA(image.Rect(0, 0, width, height))
	black := color.RGBA{0, 0, 0, 255}
	draw.Draw(m, m.Bounds(), &image.Uniform{black}, image.Pt(0, 0), draw.Src)

	var img image.Image = m
	if err := writeImage(w, &img, "png"); err != nil {
		message := "the server encountered a problem and could not process your request"
		errorResponse(w, http.StatusInternalServerError, message)
	}
}

func (app *application) jpgHandler(w http.ResponseWriter, r *http.Request) {
	width_px := chi.URLParam(r, "width_px")
	height_px := chi.URLParam(r, "height_px")
	if width_px == "" || height_px == "" {
		message := "width_px or height_px not specified"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}
	width, err := strconv.Atoi(width_px)
	if err != nil {
		message := "width of an image must be a non negative integer"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}
	height, err := strconv.Atoi(height_px)
	if err != nil {
		message := "height of an image must be a non negative integer"
		errorResponse(w, http.StatusBadRequest, message)
		return
	}

	app.msgCh <- msg{
		height: height,
		width:  width,
	}

	m := image.NewRGBA(image.Rect(0, 0, width, height))
	black := color.RGBA{0, 0, 0, 255}
	draw.Draw(m, m.Bounds(), &image.Uniform{black}, image.Pt(0, 0), draw.Src)

	var img image.Image = m
	if err := writeImage(w, &img, "jpg"); err != nil {
		message := "the server encountered a problem and could not process your request"
		errorResponse(w, http.StatusInternalServerError, message)
	}
}

func (app *application) statsHandler(w http.ResponseWriter, r *http.Request) {
	respChan := make(chan stats)
	app.msgCh <- msg{
		responseChan: respChan,
	}
	data := <-respChan
	writeJSON(w, http.StatusOK, envelope{"stats": data}, nil)
}

func main() {
	msgChannelSize := 512
	msgChan := make(chan msg, msgChannelSize)

	wg := &sync.WaitGroup{}

	app := application{
		msgCh: msgChan,
		wg:    wg,
	}
	wg.Add(1)
	go app.startApplication()

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Get("/stats", app.statsHandler)
	router.Route("/generate", func(r1 chi.Router) {
		r1.Get("/png/{width_px}/{height_px}", app.pngHandler)
		r1.Get("/jpg/{width_px}/{height_px}", app.jpgHandler)
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
