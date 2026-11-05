FROM registry.example.com/base/shiny:1
USER 0
WORKDIR /app
COPY source/ /app/
RUN R -e 'p <- trimws(readLines("packages.txt")); p <- p[nzchar(p) & !startsWith(p, "#")]; install.packages(p, repos=Sys.getenv("R_REPOSITORY", "https://cloud.r-project.org")); stopifnot(all(p %in% rownames(installed.packages())))'
RUN chgrp -R 0 /app && chmod -R g=u /app
ENV HOME=/tmp
USER 1001
EXPOSE 3838
CMD ["R","-e","shiny::runApp('.', host='0.0.0.0', port=3838)"]
