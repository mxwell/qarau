FROM postgres:18-bookworm

RUN apt update && apt install -y curl 

RUN curl -LO https://github.com/darkhanakh/pg-kazsearch/releases/download/v2.3.0/postgresql-18-pg-kazsearch_2.3.0_amd64.deb && dpkg -i postgresql-18-pg-kazsearch_2.3.0_amd64.deb && rm postgresql-18-pg-kazsearch_2.3.0_amd64.deb

