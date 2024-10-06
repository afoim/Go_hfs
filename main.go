package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

const (
	uploadPath    = "./files"
	port          = 8080
	maxUploadSize = 10 << 30 // 10 GB
)

func main() {
	// 确保上传目录存在
	if err := os.MkdirAll(uploadPath, os.ModePerm); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", listFilesHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/download/", downloadHandler)
	http.HandleFunc("/check-filename", checkFilenameHandler) // 新增的处理函数

	fmt.Printf("Server is running on http://localhost:%d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func listFilesHandler(w http.ResponseWriter, r *http.Request) {
	files, err := filepath.Glob(filepath.Join(uploadPath, "*"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for i, file := range files {
		files[i] = filepath.Base(file)
	}

	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>AcoFork - go-hfs</title>
    <style>
        #progress-bar {
            width: 300px;
            height: 20px;
            background-color: #f0f0f0;
            border-radius: 10px;
            margin-top: 10px;
        }
        #progress {
            width: 0%;
            height: 100%;
            background-color: #4CAF50;
            border-radius: 10px;
            text-align: center;
            line-height: 20px;
            color: white;
        }
    </style>
</head>
<body>
    <h1>已上传的文件喵</h1>
    <ul>
    {{range .}}
        <li><a href="/download/{{.}}">{{.}}</a></li>
    {{end}}
    </ul>
    <h2>在这里上传喵</h2>
    <form id="upload-form" action="/upload" method="post" enctype="multipart/form-data">
        <input type="file" name="file" required>
        <input type="submit" value="戳我上传喵">
    </form>
    <div id="progress-bar" style="display: none;">
        <div id="progress"></div>
    </div>
	<p>特别鸣谢：ChatGPT、Claude、Golang、我的大脑、我的手、我的笔记本、我的N100、我的键盘、我的鼠标、所有帮助\鞭策我的群u</p>

    <script>
        document.getElementById('upload-form').onsubmit = function() {
            var fileInput = document.querySelector('input[type="file"]');
            var file = fileInput.files[0];
            
            // 先检查文件名
            fetch('/check-filename?filename=' + encodeURIComponent(file.name))
                .then(response => response.json())
                .then(data => {
                    if (data.exists) {
                        alert('File already exists. Please choose a different file.');
                        return;
                    }
                    
                    // 如果文件不存在，继续上传
                    var formData = new FormData();
                    formData.append('file', file);

                    var xhr = new XMLHttpRequest();
                    xhr.open('POST', '/upload', true);

                    xhr.upload.onprogress = function(e) {
                        if (e.lengthComputable) {
                            var percentComplete = (e.loaded / e.total) * 100;
                            document.getElementById('progress-bar').style.display = 'block';
                            document.getElementById('progress').style.width = percentComplete + '%';
                            document.getElementById('progress').textContent = percentComplete.toFixed(2) + '%';
                        }
                    };

                    xhr.onload = function() {
                        if (xhr.status === 200) {
                            window.location.reload();
                        } else {
                            alert('Upload failed. ' + xhr.responseText);
                        }
                    };

                    xhr.send(formData);
                })
                .catch(error => {
                    console.error('Error:', error);
                    alert('An error occurred while checking the filename.');
                });

            return false;
        };
    </script>
</body>
</html>
`
	t := template.Must(template.New("filelist").Parse(tmpl))
	t.Execute(w, files)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 设置最大上传大小
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := header.Filename
	filePath := filepath.Join(uploadPath, filename)

	// 检查文件是否已存在
	if _, err := os.Stat(filePath); err == nil {
		http.Error(w, "File already exists", http.StatusBadRequest)
		return
	}

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// 使用 io.Copy 来复制文件内容，这样可以处理大文件而不会消耗过多内存
	_, err = io.Copy(dst, file)
	if err != nil {
		os.Remove(filePath) // 如果复制失败，删除部分上传的文件
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(r.URL.Path)
	filePath := filepath.Join(uploadPath, filename)

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to get file info", http.StatusInternalServerError)
		return
	}

	fileSize := fileInfo.Size()

	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var start, end int64
		fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

		if end == 0 {
			end = fileSize - 1
		}

		if start > end || start < 0 || end >= fileSize {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		file.Seek(start, io.SeekStart)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	io.Copy(w, file)
}

// 新增的处理函数，用于检查文件名是否已存在
func checkFilenameHandler(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		http.Error(w, "Filename is required", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(uploadPath, filename)
	exists := false
	if _, err := os.Stat(filePath); err == nil {
		exists = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"exists": exists})
}