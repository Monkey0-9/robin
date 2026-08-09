FROM rocker/r-ver:4.3.2

# Install system dependencies
RUN apt-get update -qq && apt-get install -y \
    libssl-dev \
    libcurl4-gnutls-dev \
    libxml2-dev \
    libsodium-dev \
    && rm -rf /var/lib/apt/lists/*

# Install required R packages for statistical computing & API
RUN R -e "install.packages(c('plumber', 'jsonlite'), repos='https://cloud.r-project.org/')"

WORKDIR /app

COPY . /app

EXPOSE 8088

ENTRYPOINT ["Rscript", "main.R", "--serve", "8088"]
