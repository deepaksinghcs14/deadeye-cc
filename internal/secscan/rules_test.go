package secscan

import "testing"

func hasRule(findings []Finding, name string) bool {
	for _, f := range findings {
		if f.Rule == name {
			return true
		}
	}
	return false
}

func TestScanFires(t *testing.T) {
	cases := []struct {
		name string
		path string
		rule string
		body string
	}{
		{"go-sql-concat", "handlers.go", "sql-concat", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`},
		{"go-sql-sprintf", "handlers.go", "sql-concat", `q := fmt.Sprintf("SELECT * FROM users WHERE name=%s", name)`},
		{"go-shell-interp", "run.go", "shell-interp", `cmd := exec.Command("sh", "-c", "echo "+userInput)`},
		{"py-sql-fstring", "views.py", "sql-concat", `cur.execute(f"SELECT * FROM users WHERE name={name}")`},
		{"py-shell-true", "run.py", "shell-interp", `subprocess.run(cmd, shell=True)`},
		{"py-eval", "app.py", "eval-dynamic", `result = eval(user_expr)`},
		{"java-sql-concat", "Dao.java", "sql-concat", `stmt.executeQuery("SELECT * FROM users WHERE name='" + name + "'");`},
		{"java-deser", "App.java", "java-deser", `ObjectInputStream ois = new ObjectInputStream(in); Object o = ois.readObject();`},
		{"rust-sql-format", "db.rs", "sql-concat", `let q = format!("SELECT * FROM users WHERE name={}", name);`},
		{"rust-shell-interp", "run.rs", "shell-interp", `Command::new("sh").arg("-c").arg(user_input).spawn()?;`},
		{"js-sql-template", "db.js", "sql-concat", "const q = `SELECT * FROM users WHERE name=${name}`;"},
		{"js-shell-interp", "run.js", "shell-interp", `exec("echo " + userInput);`},
		{"js-eval", "app.js", "eval-dynamic", `eval(userExpr);`},
		{"js-html-inject", "app.tsx", "html-inject", `el.innerHTML = userBio;`},
		{"tls-off-go", "client.go", "tls-off", `tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}`},
		{"tls-off-py", "client.py", "tls-off", `requests.get(url, verify=False)`},
		{"secret-literal-aws", "config.go", "secret-literal", "key := \"AKIA" + "ABCDEFGHIJKLMNOP\""}, // split so it doesn't read as a literal key to secret scanners
		{"secret-literal-pem", "config.go", "secret-literal", "-----BEGIN RSA PRIVATE KEY-----"},
		{"secret-literal-field", "config.py", "secret-literal", `password = "hunter2fake"`},
		{"weak-crypto-near-secret", "auth.go", "weak-crypto", "hash := md5.Sum([]byte(password))"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Scan(c.path, c.body, nil)
			if !hasRule(got, c.rule) {
				t.Errorf("Scan(%q, %q) = %v, want rule %q to fire", c.path, c.body, got, c.rule)
			}
		})
	}
}

func TestScanDoesNotFire(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
	}{
		{"go-parameterized-query", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name=?", name)`},
		{"go-sql-no-variable", "handlers.go", `rows, err := db.Query("SELECT * FROM users")`},
		{"md5-non-secret-context", "hash.go", "checksum := md5.Sum(fileBytes)\nlog.Println(\"computed\")\nreturn checksum"},
		{"rust-unsafe-block-alone", "mem.rs", `unsafe { *ptr = 1; }`},
		{"placeholder-secret", "config.example.py", `password = "changeme"`},
		{"go-pattern-in-python-file", "app.py", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`},
		{"python-pattern-in-go-file", "handlers.go", `cur.execute(f"SELECT * FROM users WHERE name={name}")`},
		{"rust-shell-interp-in-js-file", "run.js", `Command::new("sh").arg("-c").arg(user_input).spawn()?;`},
		{"js-regexp-exec-is-not-shell-exec", "app.js", "const m = someRegex.exec(line + \"\\n\");"},
		{"tls-off-unrelated-verify-flag", "signup.py", `email_verify = False`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Scan(c.path, c.body, nil)
			if len(got) != 0 {
				t.Errorf("Scan(%q, %q) = %v, want no findings", c.path, c.body, got)
			}
		})
	}
}

func TestScanDisabledRule(t *testing.T) {
	body := "key := \"AKIA" + "ABCDEFGHIJKLMNOP\"" // split so it doesn't read as a literal key to secret scanners
	if got := Scan("config.go", body, map[string]bool{"secret-literal": true}); len(got) != 0 {
		t.Errorf("disabled rule still fired: %v", got)
	}
}

func TestScanDedupesByName(t *testing.T) {
	// go-sql-concat and go-sql-sprintf both feed the same "sql-concat"
	// Name -- a body tripping both must still report it once.
	body := `q1 := "SELECT * FROM u WHERE n='" + name + "'"
q2 := fmt.Sprintf("SELECT * FROM u WHERE n=%s", name)`
	got := Scan("handlers.go", body, nil)
	count := 0
	for _, f := range got {
		if f.Rule == "sql-concat" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("sql-concat reported %d times, want 1: %v", count, got)
	}
}
