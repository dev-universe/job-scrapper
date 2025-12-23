package scrapper

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	ccsv "github.com/tsak/concurrent-csv-writer"
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
	var baseURL string = "https://www.saramin.co.kr/zf_user/search/recruit?searchword=" + term

	var jobs []extractedJob
	c := make(chan []extractedJob)
	totalPages := getPages(baseURL)

	for i := 0; i < totalPages; i++ {
		go getPage(i, baseURL, c)
	}

	for i := 0; i < totalPages; i++ {
		extractedJobs := <-c
		jobs = append(jobs, extractedJobs...)
	}

	writeJobs(jobs)
	fmt.Println("Done, extracted", len(jobs))
}

func writeJobs(jobs []extractedJob) {

	csv, err := ccsv.NewCsvWriter("job.csv")
	checkCSVWriteErr(err)

	defer csv.Close()
	done := make(chan bool)

	headers := []string{"ID", "Title", "Location", "Company", "Condition", "ExpireDate"}

	wErr := csv.Write(headers)
	checkErr(wErr)

	for _, job := range jobs {
		go writeJobSlice(csv, job, done)
	}

}

func writeJobSlice(w *ccsv.CsvWriter, job extractedJob, done chan bool) {
	jobSlice := []string{"https://www.saramin.co.kr/zf_user/jobs/relay/view?&rec_idx=" + job.id, job.title, job.location, job.company, job.condition, job.expireDate}
	jwErr := w.Write(jobSlice)
	checkErr(jwErr)
	done <- true
}

func getPage(page int, url string, mainC chan<- []extractedJob) {
	var jobs []extractedJob
	c := make(chan extractedJob)
	pageURL := url + "&recruitPage=" + strconv.Itoa(page+1)
	fmt.Println("Requesting", pageURL)
	res, err := http.Get(pageURL)
	checkErr(err)
	checkCode(res)

	defer res.Body.Close() // prevent memory leaks

	doc, err := goquery.NewDocumentFromReader(res.Body)
	checkErr(err)

	searchCards := doc.Find(".item_recruit")

	searchCards.Each(func(i int, card *goquery.Selection) {
		go extractJob(card, c)
	})

	for i := 0; i < searchCards.Length(); i++ {
		job := <-c
		jobs = append(jobs, job)
	}

	mainC <- jobs
}

func getPages(url string) int {
	pages := 0
	res, err := http.Get(url)
	checkErr(err)
	checkCode(res)

	defer res.Body.Close() // prevent memory leaks

	doc, err := goquery.NewDocumentFromReader(res.Body)
	checkErr(err)

	doc.Find(".pagination").Each(func(i int, s *goquery.Selection) {
		pages = s.Find("a").Length()
	})
	return pages
}

func checkErr(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func checkCSVWriteErr(err error) {
	if err != nil {
		panic("Could not open csv file for writing")
	}
}

func checkCode(res *http.Response) {
	if res.StatusCode != 200 {
		log.Fatalln("Request failed with status: ", res.StatusCode)
	}
}

func extractJob(card *goquery.Selection, c chan<- extractedJob) {
	id, _ := card.Attr("value")
	title := cleanString(card.Find(".job_tit>a").Text())
	location := cleanString(card.Find(".job_condition>span>a").Text())
	company := cleanString(card.Find(".area_corp>strong>a").Text())
	condition := cleanString(card.Find(".job_condition>span").Text())
	expireDate := cleanString(card.Find(".job_date>span").Text())
	c <- extractedJob{
		id:         id,
		title:      title,
		location:   location,
		company:    company,
		condition:  condition,
		expireDate: expireDate}
}

func cleanString(str string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(str)), " ")
}
