# Proxy-Go

A simple reverse proxy server written in Go.

## Configuration

Create a `config.json` file in the `data` directory:

```json
{
  "MAP":{
      "/path1": "https://path1.com/path/path/path",
      "/path2": "https://path2.com"
    }
}
```
