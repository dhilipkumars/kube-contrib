package main

import (
	"html/template"
	"log"
	"net/http"
)

const HTML = `
<!DOCTYPE html>
<html>
<head>
<style>
#customers {
font-family: "Trebuchet MS", Arial, Helvetica, sans-serif;
border-collapse: collapse;
width: 100%;
}

#customers td, #customers th {
border: 1px solid #ddd;
padding: 8px;
text-align: center;
}

#customers tr:nth-child(even){background-color: #f2f2f2;}

#customers tr:hover {background-color: #ddd;}

#customers th {
padding-top: 12px;
padding-bottom: 12px;
text-align: center;
background-color: #4CAF50;
color: white;
}
</style>
</head>
<body>
<h1><center> Huawei-Paas contribution to K8s </center></h1>
<table id="customers">
<tr>
<th>S.No</th>
<th>Github Handle</th>
<th>Commits</th>
</tr>
{{range .Rec}}
<tr>
<td>{{.Index}}</td>
<td>{{.Name}}</td>
<td>{{.Commits}}</td>
</tr>
{{end}}
</table>
<h5>Updated:{{.TimeUpdate}}</h5>
</body>
</html>`

type RecordOutput struct {
	Index   int
	Name    string
	Commits int
}

type TableOutput struct {
	Rec        []RecordOutput
	TimeUpdate string
}

func StartServer (t *Table) {

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var T TableOutput

		recs := t.Tabulate()
		for i, rec := range recs {
			T.Rec = append(T.Rec, RecordOutput{Index:i+1, Name:rec.Name, Commits:rec.Commits})
		}
		T.TimeUpdate = t.Update.String()

		tpl, err := template.New("webpage").Parse(HTML)
		if err != nil {
			log.Printf("ServePage()-> Error occured =%v", err)
			return
		}
		err = tpl.Execute(w,T)

	})

	http.ListenAndServe(HTTPServerPort, nil)
}