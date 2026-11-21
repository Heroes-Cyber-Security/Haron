# syntax=docker/dockerfile:1

# The mock only uses the standard library, so the alpine base is enough.
FROM python:3.13.7-alpine

RUN adduser -D -u 1000 ctf

WORKDIR /home/ctf

COPY app.py /home/ctf/app.py

USER ctf

CMD [ "python", "app.py" ]
