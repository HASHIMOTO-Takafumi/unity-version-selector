package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/shibukawa/configdir"
	"golang.org/x/sys/windows/registry"
)

type Config struct {
	ProgramDir string
	DirPattern string
	Versions   map[string]string
}

const vendorName = "hasht"
const applicationName = "unity-version-selector"
const configFile = "config.toml"
const depthCutoff = 6
const versionPattern = `m_EditorVersion: (.+)`

var defaultConfig = Config{
	ProgramDir: "C:/Program Files",
	DirPattern: `^Unity(.+)$`,
	Versions:   map[string]string{},
}

func main() {
	reload := flag.Bool("reload", false, "Reload the Unity versions")
	list := flag.Bool("list", false, "Show the list of the Unity versions")
	flag.Parse()

	var project string
	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		project = os.Args[1]
	}

	configDirs := configdir.New(vendorName, applicationName)
	configDir := configDirs.QueryFolders(configdir.Global)[0]
	configPath := filepath.Join(configDir.Path, configFile)

	var config Config
	if *reload {
		config.initialize(*configDir)
	} else if _, err := toml.DecodeFile(configPath, &config); err != nil {
		config.initialize(*configDir)
	}

	if *list {
		for _, ver := range config.getVersionKeys() {
			fmt.Println(ver, ":", config.Versions[ver])
		}
		return
	}

	if project == "" {
		project = askProject()
	}

	config.openProject(project)
}

func askProject() string {
	recents := getRecentProjects()
	for i, path := range recents {
		println(i, ":", path)
	}

	print("\n> ")
	stdin := bufio.NewScanner(os.Stdin)
	stdin.Scan()

	index, err := strconv.Atoi(stdin.Text())
	if err != nil {
		log.Fatal(err)
	}
	if index < 0 || index >= len(recents) {
		log.Fatal("The index is out of range")
	}

	return recents[index]
}

func getRecentProjects() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Unity Technologies\Unity Editor 5.x`, registry.QUERY_VALUE)
	if err != nil {
		log.Fatal(err)
	}
	defer k.Close()

	names, err := k.ReadValueNames(0)
	if err != nil {
		log.Fatal(err)
	}

	r := []string{}
	for _, name := range names {
		if !strings.HasPrefix(name, "RecentlyUsedProjectPaths") {
			continue
		}

		val, _, err := k.GetBinaryValue(name)
		if err != nil {
			panic(err)
		}

		r = append(r, string(val[:len(val)-1]))
	}

	return r
}

func getProjectVersion(projectPath string) string {
	p := filepath.Join(projectPath, "ProjectSettings/ProjectVersion.txt")

	if !isExists(p) {
		log.Fatal("No ProjectVersion.txt")
	}

	data, err := ioutil.ReadFile(p)
	if err != nil {
		log.Fatal(err)
	}

	r := regexp.MustCompile(versionPattern)
	s := r.FindSubmatch(data)
	if len(s) < 2 {
		log.Fatal("ProjectVersion.txt doesn't have m_EditorVersion")
	}

	return string(s[1])
}

func (config Config) openProject(path string) {
	version := getProjectVersion(path)

	exe, ok := config.Versions[version]
	if !ok {
		log.Fatal("The version not found")
	}

	cmd := exec.Command(exe, "-projectPath", path)
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
}

func (config *Config) initialize(configDir configdir.Config) {
	*config = defaultConfig
	(*config).Versions = loadVersions(*config)
	config.output(configDir)
}

func (config Config) output(configDir configdir.Config) {
	file, err := configDir.Create(configFile)
	if err != nil {
		log.Fatal(err)
	}

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		log.Fatal(err)
	}
}

func (config Config) getVersionKeys() []string {
	keys := []string{}
	newKeys := []string{}
	for key, _ := range config.Versions {
		if strings.HasPrefix(key, "20") {
			newKeys = append(newKeys, key)
		} else {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)
	sort.Strings(newKeys)

	keys = append(keys, newKeys...)

	return keys
}

func loadVersions(config Config) map[string]string {
	versions := map[string]string{}
	r := regexp.MustCompile(config.DirPattern)

	files, err := ioutil.ReadDir(config.ProgramDir)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		name := file.Name()
		s := r.FindStringSubmatch(name)
		if len(s) >= 2 {
			p := path.Join(config.ProgramDir, name)
			exe := deepFind(p, "Unity.exe")
			if exe == "" {
				log.Println(name, "has no Unity.exe!")
				continue
			}
			versions[s[1]] = exe
		}
	}

	return versions
}

func isExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type dummyError struct{}

func (f dummyError) Error() string {
	return ""
}

func deepFind(root, name string) string {
	sep := string(filepath.Separator)

	var foundPath string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		depth := strings.Count(path, sep)
		if depth > depthCutoff {
			return filepath.SkipDir
		}

		if !info.IsDir() && info.Name() == "Unity.exe" {
			foundPath = path
			return dummyError{}
		}

		return nil
	})

	if err != (dummyError{}) {
		log.Fatal(err)
	}

	return foundPath
}
