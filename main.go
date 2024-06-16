package main

// run은 환경변수를 설정한 후 다른 프로그램 실행한다.

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// die는 받아들인 에러를 출력하고 프로그램을 종료한다.
func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// getEnv는 환경변수 리스트에서 해당 키를 찾아 그 값을 반환한다.
// 만약 키가 없다면 빈 문자열을 반환하고, 두 개 이상이라면 뒤에 설정된 값을 반환한다.
func getEnv(key string, env []string) string {
	for i := len(env) - 1; i >= 0; i-- {
		e := env[i]
		kv := strings.SplitN(e, "=", -1)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k == key {
			return v
		}
	}
	return ""
}

// parseEnv는 env 환경변수 리스트를 참조해서 e 환경변수의 값을 해석한다.
// 예를들어 e가 "TEST=$OTHER", env가 []string{"OTHER=test"} 라면
// "TEST=test", nil을 반환한다.
// e가 환경변수 문자열로 변경 불가능 하다면 빈문자열과 에러를 반환한다.
// 반환되는 문자열 키와 값 앞 뒤의 공백은 제거된다.
//
// 주의: e 값의 특정 문자는 해당 OS에 맞게 자동으로 변환된다.
// 관련해서는 autoConvertValueString 함수 주석 참조할 것.
func parseEnv(e string, env []string) (string, error) {
	kv := strings.SplitN(e, "=", -1)
	if len(kv) != 2 {
		return "", fmt.Errorf("expect key=value string: got %q", e)
	}
	k := strings.TrimSpace(kv[0])
	if k == "" {
		return "", fmt.Errorf("environment variable key not found")
	}
	if strings.Contains(k, "$") {
		return "", fmt.Errorf("environment variable key should not have '$' char: %s", k)
	}
	v := strings.TrimSpace(kv[1])
	v, err := autoConvertValueString(v, env)
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, e)
	}
	return k + "=" + v, nil
}

// autoConvertValueString는 환경변수 값 안의 특정 문자를
// 해당 OS에 맞게 자동으로 변환한다.
//
// 	/  ->  해당 OS의 경로 구분자로 변경된다.
// 	:  ->  해당 OS의 환경변수 구분자로 변경된다.
//
// 만일 이렇게 변경되지 않아야 할 문자라면 `로 감싸면 된다.
//
// 	예) FILE_PATH=`https://웹/사이트/주소`
//
func autoConvertValueString(v string, env []string) (string, error) {
	vs := strings.Split(v, "`")
	if len(vs)%2 != 1 {
		return "", fmt.Errorf("quote(`) not terminated")
	}
	for i := 0; i < len(vs); i += 2 {
		// 0, 2, 4, ... 번째 항목들이 쿼트 바깥의 문자열이다.
		vs[i] = filepath.FromSlash(vs[i])
		vs[i] = envSepFromColon(vs[i])
		vs[i] = replaceEnvVar(vs[i], env)
	}
	v = strings.Join(vs, "")
	return v, nil
}

// replaceEnvVar는 문자열 안의 환경변수들을 그 값으로 변경하여 반환한다.
func replaceEnvVar(v string, env []string) string {
	re := regexp.MustCompile(`[$]\w+`)
	for {
		idxs := re.FindStringIndex(v)
		if idxs == nil {
			break
		}
		s := idxs[0]
		e := idxs[1]
		pre := v[:s]
		post := v[e:]
		envk := v[s+1 : e]
		envv := getEnv(envk, env)
		v = pre + envv + post
	}
	return v
}

// parseEnvsetFile은 여러 환경변수 설정 파일 경로가 저장 되어있는
// envset 파일을 불러와 그 경로를 파싱한다. 만일 가리키는 경로에
// 환경변수가 포함되어 있다면 이를 실제경로로 변경한다.
func parseEnvsetFile(f string, env []string) ([]string, error) {
	if f == "" {
		return []string{}, nil
	}
	var err error
	f, err = filepath.Abs(f)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}
	s := string(b)
	envfiles := make([]string, 0)
	for _, envf := range strings.Split(s, "\n") {
		envf = strings.TrimSpace(envf)
		if envf == "" {
			continue
		}
		if strings.HasPrefix(envf, "#") {
			continue
		}
		envf = replaceEnvVar(envf, env)
		if !filepath.IsAbs(envf) {
			envf = filepath.Join(filepath.Dir(f), envf)
		}
		envfiles = append(envfiles, envf)
	}
	return envfiles, nil
}

// parseEnvFile은 파일을 읽어 그 안의 환경변수 문자열을 리스트 형태로 반환한다.
// 파일을 읽는 도중 에러가 나거나 환경변수 파싱이 불가능하다면 빈문자열과 에러를 반환한다.
func parseEnvFile(f string, env []string) ([]string, error) {
	if f == "" {
		return []string{}, nil
	}
	var err error
	f, err = filepath.Abs(f)
	if err != nil {
		return nil, err
	}
	errNoFile := true
	// env 파일경로 뒤에 물음표가 붙어있으면 그 파일이 없어도 에러를 내지 않음.
	//
	// 할일: 경로 앞에 물음표를 붙이는 것도 아직 유효한데, 이를 사용하는
	// 명령이 2L 내부에 남아있기 때문이고, 이를 다 수정한 이후에는
	// 지울 것.
	if len(f) > 0 && (f[0] == '?' || f[len(f)-1] == '?') {
		errNoFile = false
		f = strings.Trim(f, "?")
	}
	b, err := ioutil.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			if errNoFile {
				return nil, err
			}
			return []string{}, nil
		}
		return nil, err
	}
	s := string(b)
	env = append([]string{
		// run에서 사용되는 특별한 환경변수 추가
		"HERE=" + filepath.Dir(f),
	}, env...)
	penv := []string{}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "#") {
			continue
		}
		e, err := parseEnv(l, env)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		penv = append(penv, e)

		// 기존 환경변수에도 추가하는데 그 이유는
		// 앞줄의 환경변수가 뒷줄의 환경변수를 완성하는데 쓰일 수 있기 때문이다.
		// 이렇게 한다고 해도 이 함수를 부른 함수의 env에는 적용되지 않는다.
		env = append(env, e)
	}
	return penv, nil
}

// Config는 커맨드라인 옵션 값을 담는다.
type Config struct {
	env      string
	envset   string
	envfile  string
	dir      string
	printLog bool
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.env, "env", "", "미리 선언할 환경변수. envset, envfile에 앞서 설정됩니다. 콤마(,)를 이용해 여러 환경변수를 설정할 수 있습니다.")
	flag.StringVar(&cfg.envset, "envset", "", "환경 파일들이 설정되어있는 환경셋 파일을 불러옵니다. 하나의 파일만 설정가능합니다. envfile에 앞서 파싱됩니다.")
	flag.StringVar(&cfg.envfile, "envfile", "", "환경변수들이 설정되어있는 파일을 불러옵니다. 콤마(,)를 이용해 여러 파일을 불러 올 수 있습니다. 파일명 뒤에 ?(물음표)를 붙이면 파일이 없어도 에러가 나지 않습니다.")
	flag.StringVar(&cfg.dir, "dir", "", "명령을 실행할 디렉토리를 설정합니다. 설정하지 않으면 현재 디렉토리에서 실행합니다.")
	flag.BoolVar(&cfg.printLog, "log", false, "run에서 설정된 환경변수를 출력합니다.")
	flag.Parse()

	// OS 환경변수에 env와 envfile을 파싱해 환경변수를 추가/대체한다.
	env := os.Environ()
	envs := []string{}
	for _, e := range strings.Split(cfg.env, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		var err error
		e, err = parseEnv(e, env)
		if err != nil {
			die(err)
		}
		envs = append(envs, e)
	}
	if cfg.printLog {
		fmt.Println("-env")
		for _, e := range envs {
			fmt.Printf("  %s\n", e)
		}
	}
	for _, e := range envs {
		env = append(env, e)
	}

	// 파싱할 env 파일을 찾는다.
	// 우선 envset 파일을 파싱하고 다음으로 envfile에 설정된 파일들을 추가한다.
	envfiles, err := parseEnvsetFile(cfg.envset, env)
	if err != nil {
		die(err)
	}
	for _, envf := range strings.Split(cfg.envfile, ",") {
		envf = strings.TrimSpace(envf)
		if envf == "" {
			continue
		}
		envfiles = append(envfiles, envf)
	}
	for _, envf := range envfiles {
		envs, err := parseEnvFile(envf, env)
		if err != nil {
			die(err)
		}
		if cfg.printLog {
			fmt.Printf("\n%s\n", envf)
			for _, e := range envs {
				fmt.Printf("  %s\n", e)
			}
		}
		for _, e := range envs {
			env = append(env, e)
		}
	}

	// 설정된 환경으로 명령을 실행한다.
	cmds := flag.Args()
	if len(cmds) == 0 {
		die(errors.New("need command to run"))
	}
	cmd := exec.Command(cmds[0], cmds[1:]...)
	cmd.Env = env
	cmd.Dir = cfg.dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		die(err)
	}
}
