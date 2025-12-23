package scrapper

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type extractedJob struct {
	id         string
	title      string
	location   string
	company    string
	condition  string
	expireDate string
}

func Scrape(term string) {
	baseURL := "https://www.saramin.co.kr/zf_user/search/recruit?searchword=" + term

	totalPages := getPages(baseURL)
	if totalPages <= 0 {
		fmt.Println("No pages found")
		return
	}

	// 1) 워커 풀 설정 (페이지 단위 병렬)
	const pageWorkers = 6 // 환경에 따라 4~12 정도로 조절 권장
	pagesCh := make(chan int)
	jobsCh := make(chan extractedJob, 200) // 버퍼는 적당히 크게

	// 2) 단일 CSV writer 고루틴
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		writeJobsFromChannel(jobsCh)
	}()

	// 3) 페이지 워커들
	var workersWG sync.WaitGroup
	workersWG.Add(pageWorkers)
	for w := 0; w < pageWorkers; w++ {
		go func(workerID int) {
			defer workersWG.Done()
			for page := range pagesCh {
				pageURL := baseURL + "&recruitPage=" + strconv.Itoa(page)

				// 네트워크/서버 상황 대비: 간단한 속도 제한(선택)
				// 너무 공격적으로 때리면 차단/응답불량 가능성이 있어 약간의 지연을 둔다
				time.Sleep(50 * time.Millisecond)

				jobs := getPageJobs(pageURL)
				for _, job := range jobs {
					jobsCh <- job
				}
			}
		}(w)
	}

	// 4) 작업 큐에 페이지 넣기 (사람인 페이지는 1부터)
	go func() {
		for p := 1; p <= totalPages; p++ {
			pagesCh <- p
		}
		close(pagesCh)
	}()

	// 5) 워커 종료 대기 후 jobsCh 닫아서 writer 종료 유도
	workersWG.Wait()
	close(jobsCh)

	// 6) writer 종료 대기
	writerWG.Wait()

	fmt.Println("Done")
}

// 페이지 하나에서 모든 공고를 뽑아 slice로 반환
func getPageJobs(pageURL string) []extractedJob {
	fmt.Println("Requesting", pageURL)

	res, err := http.Get(pageURL)
	checkErr(err)
	checkCode(res)
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	checkErr(err)

	var jobs []extractedJob
	doc.Find(".item_recruit").Each(func(i int, card *goquery.Selection) {
		jobs = append(jobs, extractJob(card))
	})
	return jobs
}

// 단일 writer: jobsCh에서 읽어서 CSV로 기록
func writeJobsFromChannel(jobsCh <-chan extractedJob) {
	f, err := os.Create("jobs.csv")
	checkErr(err)
	defer f.Close()

	// UTF-8 BOM: 파일 맨 앞에 1회 기록
	utf8bom := []byte{0xEF, 0xBB, 0xBF}
	_, err = f.Write(utf8bom)
	checkErr(err)

	w := csv.NewWriter(f)
	defer w.Flush()

	headers := []string{"ID", "Title", "Location", "Company", "Condition", "ExpireDate"}
	checkErr(w.Write(headers))

	count := 0
	for job := range jobsCh {
		row := []string{
			"https://www.saramin.co.kr/zf_user/jobs/relay/view?&rec_idx=" + job.id,
			job.title,
			job.location,
			job.company,
			job.condition,
			job.expireDate,
		}
		checkErr(w.Write(row))
		count++
	}

	// Flush 에러 확인
	checkErr(w.Error())
}

func getPages(url string) int {
	res, err := http.Get(url)
	checkErr(err)
	checkCode(res)
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	checkErr(err)

	// NOTE: 기존 방식(.pagination a Length)은 사이트 UI 변경에 취약합니다.
	// 그래도 기존 로직을 유지하되, 최소 1페이지 방어 로직을 추가합니다.
	pages := 0
	doc.Find(".pagination").Each(func(i int, s *goquery.Selection) {
		pages = s.Find("a").Length()
	})

	// pagination이 없을 때(검색 결과 1페이지만) 방어
	if pages == 0 {
		pages = 1
	}
	return pages
}

// 고루틴 없이 단일 추출 함수로 단순화
func extractJob(card *goquery.Selection) extractedJob {
	id, _ := card.Attr("value")
	title := CleanString(card.Find(".job_tit>a").Text())
	location := CleanString(card.Find(".job_condition>span>a").Text())
	company := CleanString(card.Find(".area_corp>strong>a").Text())
	condition := CleanString(card.Find(".job_condition>span").Text())
	expireDate := CleanString(card.Find(".job_date>span").Text())

	return extractedJob{
		id:         id,
		title:      title,
		location:   location,
		company:    company,
		condition:  condition,
		expireDate: expireDate,
	}
}

// CleanString cleans a string
func CleanString(str string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(str)), " ")
}

func checkErr(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

// func checkCSVWriteErr(err error) {
// 	if err != nil {
// 		panic("Could not open csv file for writing")
// 	}
// }

func checkCode(res *http.Response) {
	if res.StatusCode != 200 {
		log.Fatalln("Request failed with status: ", res.StatusCode)
	}
}
