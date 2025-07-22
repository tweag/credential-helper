import requests

if __name__ == '__main__':
    res = requests.get("https:/httpbin.org/get")
    print(res)
