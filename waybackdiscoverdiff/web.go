package waybackdiscoverdiff

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// """Check for current simhash processing tasks for target url & year
// """
func GetActiveTask(ctx context.Context, rdb *redis.Client, url, year string) (*TaskStatus, error) {
	key := makeStatusKey(url, year)

	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var status TaskStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		return nil, err
	}
	fmt.Println(status)
	return &status, nil
}

func GetTaskStatus(ctx context.Context, rdb *redis.Client, url, year string) (*TaskStatus, error) {
	if url == "" || year == "" {
		log.Printf("GetTaskStatus called with empty url or year")
		return nil, fmt.Errorf("url and year are required")
	}
	
	key := makeStatusKey(url, year)
	log.Printf("Getting task status with key: %s", key)

	val, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		// Key does not exist; return nil and no error
		log.Printf("No task status found for key: %s", key)
		return nil, nil
	}
	if err != nil {
		log.Printf("Error getting task status: %v", err)
		return nil, err
	}

	log.Printf("Task status found: %s", val)
	var status TaskStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		log.Printf("Error unmarshaling task status: %v", err)
		return nil, err
	}
	log.Printf("Task status unmarshaled: %+v", status)
	return &status, nil
}

// """Return simhash data for specific URL and year (optional),
// page is also optional.
// """

func ServeRoot(w http.ResponseWriter, r *http.Request) {
	version := "v0.1.0"
	fmt.Fprintf(w, "wayback-discover-diff service version: %s", version)
}

// """Return simhash data for specific URL and year (optional),
// page is also optional.
// """
func ServeSimhash(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		StatsdInc("get-simhash-year-request", 1)
		params := r.URL.Query()
		url_ := params.Get("url")
		timestamp_ := params.Get("timestamp")
		year_ := params.Get("year")
		page_ := params.Get("page")
		compress_ := params.Get("compress")

		page, _ := strconv.Atoi(page_)
		if url_ == "" {
			resp := HttpResponse{Status: "error", Info: "url param is required."}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		if !UrlIsValid(&url_) {
			resp := HttpResponse{Status: "error", Info: "invalid url format."}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		ctx := context.Background()

		if timestamp_ == "" {
			if year_ == "" {
				resp := HttpResponse{Status: "error", Info: "year param is required."}
				writeJSON(w, http.StatusOK, resp)
				return
			}
			snapshotsPerPage_ := GetConfig("snapshots.number_per_page").(int)
			res, _, err := YearSimhash(rdb, url_, year_, page, snapshotsPerPage_)
			if err != nil {
				slog.Error("Cannot get simhash of", "url", url_, "error", err)
				resp := HttpResponse{Status: "error", Info: err.Error()}
				writeJSON(w, http.StatusInternalServerError, resp)
				return
			}

			if len(res) > 0 {
				writeJSON(w, http.StatusOK, res)
				return
			}

			task, _ := GetTaskStatus(ctx, rdb, url_, year_)

			output := map[string]any{"captures": res[0], "totalCaptures": res[1], "status": task.Status}

			if compress_ == "true" || compress_ == "1" {
				captures, ok := output["captures"].([][2]string)
				if !ok {
					resp := HttpResponse{Status: "success", Info: "invalid capture format"}
					writeJSON(w, http.StatusInternalServerError, resp)
					return
				}
				compressed, hashes := CompressCaptures(captures)
				output["captures"] = compressed
				output["hashes"] = hashes
			}
			resp := HttpResponse{Status: "success", Info: output}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		results := GetTimestampSimhash(rdb, url_, timestamp_)

		writeJSON(w, http.StatusOK, results)
	}
}

func writeJSON(w http.ResponseWriter, code int, resp any) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(resp)
}

// """Start simhash calculation for URL & year.
// Validate parameters url & timestamp before starting Celery task.
// """
func ServeCalculateSimhash(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		StatsdInc("calculate-simhash-year-request", 1)
		params := r.URL.Query()
		url_ := params.Get("url")
		year_ := params.Get("year")
		ctx := context.Background()

		if url_ == "" {
			resp := HttpResponse{Status: "error", Info: "url param is required."}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		if !UrlIsValid(&url_) {
			resp := HttpResponse{Status: "error", Info: "invalid url format."}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		if year_ == "" || len(year_) != 4 || !regexp.MustCompile(`^\d{4}$`).MatchString(year_) {
			resp := HttpResponse{Status: "error", Info: "year param is required."}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		task, err := GetTaskStatus(ctx, rdb, url_, year_)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, HttpResponse{
				Status: "error",
				Info:   "failed to get task status: " + err.Error(),
			})
			return
		}
		if task != nil {
			if task.Status != "SUCCESS" {
				resp := HttpResponse{Status: task.Status, JobId: task.ID}
				writeJSON(w, http.StatusOK, resp)
				return
			}
		}

		jobId := uuid.New().String()
		log.Printf("ServeCalculateSimhash: Generated new job ID: %s", jobId)
		discoverTask, err := NewDiscoverTask(url_, year_, jobId, time.Now())
		if err != nil {
			log.Printf("ServeCalculateSimhash: Error creating discover task: %v", err)
			writeJSON(w, http.StatusInternalServerError, HttpResponse{Status: "error", Info: "error creating task"})
			return
		}
	
		info, err := AsynqClient.Enqueue(discoverTask, asynq.Queue("wayback_discover_diff"))
		if err != nil {
			log.Printf("ServeCalculateSimhash: Error enqueueing task: %v", err)
			writeJSON(w, http.StatusInternalServerError, HttpResponse{Status: "error", Info: "error enqueueing task"})
			return
		}
		log.Printf("ServeCalculateSimhash: Task enqueued successfully: %s", info.ID)
	
		SetJobStatus(ctx, rdb, jobId, url_, year_, "PENDING")
		err = SetTaskStatus(ctx, rdb, TypeDiscover, url_, year_, "PENDING", "Started the task", jobId)
		if err != nil {
			log.Println(err.Error())
		}
		resp := HttpResponse{Status: "started", JobId: jobId}
		writeJSON(w, http.StatusOK, resp)
	}
}

// """Return job status.
// """
func ServeJob(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		StatsdInc("status-request", 1)
		jobId := r.URL.Query().Get("job_id")
		ctx := context.Background()

		if jobId == "" {
			resp := HttpResponse{Status: "error", Info: "job_id param is required."}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		jobGot := GetJobStatus(ctx, rdb, jobId)
		if jobGot == "" {
			resp := HttpResponse{
				Status: "error",
				Info:   "job status not found for job_id: " + jobId,
			}
			writeJSON(w, http.StatusNotFound, resp)
			return
		}

		jobGotArray := strings.Split(jobGot, "|")
		if len(jobGotArray) != 3 {
			resp := HttpResponse{
				Status: "error",
				Info:   "job status format invalid for job_id: " + jobId,
			}
			writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		jobStatus := jobGotArray[0]
		url := jobGotArray[1]
		year := jobGotArray[2]

		task, err := GetTaskStatus(ctx, rdb, url, year)
		if err != nil {
			log.Println("Task: Error", err.Error())
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if task == nil {
			writeJSON(w, http.StatusOK, HttpResponse{
				Status: jobStatus,
				JobId:  jobId,
				Info:   "task status not yet available",
			})
			return
		}
		if jobStatus == "SUCCESS" {
			resp := HttpResponse{Status: jobStatus, JobId: jobId, Duration: task.Description}
			writeJSON(w, http.StatusOK, resp)
			return
		} else {
			resp := HttpResponse{Status: jobStatus, JobId: jobId, Info: task.Description}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
}

func GetJobStatus(ctx context.Context, rdb *redis.Client, jobId string) string {
	status, err := rdb.Get(ctx, jobId).Result()
	if err == redis.Nil {
		return ""
	}
	if err != nil {
		return ""
	}
	return status
}
