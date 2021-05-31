package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/spf13/viper"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)
var logger *log.Logger
func init() {
	//日志设置
	fileWriter,err:=os.OpenFile("hoi.log",os.O_WRONLY|os.O_CREATE, 0755)
	if err!=nil {
		
		logger.Panicln(err)
		
	}
	logger=log.New(io.MultiWriter(os.Stdout,fileWriter),"",log.Lshortfile|log.LstdFlags)
	//absUrl:=GetAppPath()
	//viper.SetConfigFile(fmt.Sprintf("%s\\config.toml",absUrl))
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.SetConfigType("toml")
	err = viper.ReadInConfig()
	if err != nil {
		logger.Panicln(err)
	}
	logger.Println("已有配置:")
	fmt.Printf("%v\n",viper.AllSettings())
	logger.Println("-----------------------")

	
}

func main() {
	Run()
}

type Cache struct {
	sync.Mutex
	MaxCache   int64
	Caches     map[string]string
	LatestTime string
	CachesKeys []string
}

func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, os.Kill)
		for {
			select {
			case <-sig:
				cancel()
				return
			}
		}
	}()

	logger.Println("选择已有配置:[配置名](输入no表示新建配置)")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		logger.Panicln(err)
	}
	var (
		interval, maxCache int64
		input              map[string]interface{}
	)
	text = strings.TrimSpace(text[:len(text)-1])
	if text == "no" {
		input = InputFromStdin()
	} else {
		input = InputFromConfig(strings.ToTitle(text))
	}
	interval = input["interval"].(int64)
	maxCache = input["max_cache"].(int64)
	timeTickerChan := time.NewTicker(time.Second * time.Duration(interval))
	cache := Cache{MaxCache: maxCache, Caches: make(map[string]string)}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		logger.Println("开始监听")
		defer wg.Done()
		for {
			select {
			case _, ok := <-timeTickerChan.C:
				if !ok {
					return
				}
				logger.Println( "执行")
				Watcher(input, &cache)
			case <-ctx.Done():
				fmt.Printf("退出")
				return
			}

		}

	}()
	wg.Wait()
}

func InputFromConfig(text string) map[string]interface{} {
	result := viper.GetStringMap(text)
	if len(result) == 0 {
		logger.Println("无配置")
		Run()
	}
	return result
}

func InputFromStdin() map[string]interface{} {
	logger.Println("请输入:[前缀]|[hoi存档地址]|[时间间隔(秒)]|[最大缓存]|[保存时间间隔]")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		logger.Panicln(err)
	}
	textSplit := strings.Split(text, "|")
	prefix, url, interval, maxCache,savePeriod := textSplit[0], textSplit[1], textSplit[2], textSplit[3],textSplit[4]
	result := make(map[string]interface{})
	result["prefix"] = prefix
	result["url"] = url
	result["interval"], _ = strconv.ParseInt(interval,10,64)
	result["max_cache"], _ = strconv.ParseInt(maxCache,10,64)
	result["save_period"], _ = strconv.ParseInt(savePeriod[:len(savePeriod)-1],10,64)
	viper.Set(fmt.Sprintf("%s.url",prefix),result["url"])
	viper.Set(fmt.Sprintf("%s.interval",prefix),result["interval"])
	viper.Set(fmt.Sprintf("%s.max_cache",prefix),result["max_cache"])
	viper.Set(fmt.Sprintf("%s.save_period",prefix),result["save_period"])
	viper.Set(fmt.Sprintf("%s.prefix",prefix),result["prefix"])
	err=viper.WriteConfig()
	if err != nil {
		logger.Panicln(err)
	}
	logger.Println("保存配置成功")
	return result
}

func Watcher(config map[string]interface{}, cache *Cache) {
	url, prefix, saveTime := config["url"].(string), config["prefix"].(string), config["save_period"].(int64)
	distUrl := fmt.Sprintf("%s/autosave.hoi4", url)
	hoiTime := GetHoiTime(distUrl).Format("2006_01_02")
	newFileUrl := fmt.Sprintf("%s/%s_%s.hoi", url, prefix, hoiTime)
	cache.Add(hoiTime, newFileUrl, distUrl, saveTime)
}

func (c *Cache) Add(hoiTime string, newFileUrl string, distUrl string, saveTime int64) {
	c.Lock()
	defer c.Unlock()
	hoiToTime := StringToTime(hoiTime)
	isFirst := false
	isModify := false
	if c.LatestTime == "" {
		isFirst = true
		c.LatestTime = hoiTime
	}
	if isFirst {
		isModify = true
	} else {
		latesToTime := StringToTime(c.LatestTime)
		if hoiToTime.Sub(latesToTime) >= time.Hour*time.Duration(saveTime) {
			isModify = true
		} else {
			logger.Println(hoiTime,"时长不满足，跳过")
		}
	}
	if isModify {
		c.LatestTime = hoiTime
		logger.Println(fmt.Sprintf("%s:%s 创建", time.Now().Format("2006-01-02"), newFileUrl))
		FileCopyer(distUrl, newFileUrl)
		c.CachesKeys = append(c.CachesKeys, hoiTime)
		c.Caches[hoiTime] = newFileUrl
		if int64(len(c.Caches)) > c.MaxCache {
			delete(c.Caches, c.CachesKeys[0])
			c.CachesKeys = c.CachesKeys[1:]
			DeleteFile(c.Caches[c.CachesKeys[0]])
		}
	}
}

func StringToTime(s string) time.Time {
	time, err := time.Parse("2006_01_02", s)
	if err != nil {
		logger.Panicln(err)
	}
	return time
}

func DeleteFile(url string) {
	err := os.Remove(url)
	if err != nil {
		logger.Println("删除文件失败，请手动删除")
	}
}
func FileCopyer(from string, to string) {
	fromFileData, err := os.ReadFile(from)
	if err != nil {
		logger.Println("文件被占用，3秒后尝试")
		timer := time.NewTimer(time.Second * 3)
		defer timer.Stop()
		<-timer.C
		FileCopyer(from, to)
	}
	os.WriteFile(to, fromFileData, 0666)
}

func GetHoiTime(url string) time.Time {
	data, err := os.ReadFile(url)
	if err != nil {
		logger.Panicln(err)
	}
	reg, _ := regexp.Compile("\\d*?\\.\\d*?\\.\\d*?\\.\\d*?")

	dataString := string(SplitArray(0.8, data))
	result := reg.FindAllStringSubmatch(dataString, -1)
	var dates []time.Time
	for _, text := range result {
		createTime, _ := time.Parse("2006.1.2", text[0][:len(text[0])-1])
		dates = append(dates, createTime)
	}
	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Unix() > dates[j].Unix()
	})
	return dates[0]
}

func SplitArray(startPercent float64, array []byte) []byte {
	start := int(startPercent * float64(len(array)))
	return array[start:]
}
