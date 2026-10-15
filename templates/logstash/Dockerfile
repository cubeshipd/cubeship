# The published image's pipeline listens for Beats and prints what arrives to
# stdout. Cubeship cannot mount a file into a container, so this image puts
# logstash.conf in that pipeline's place and changes nothing else.
FROM docker.elastic.co/logstash/logstash:9.5.4
COPY logstash.conf /usr/share/logstash/pipeline/logstash.conf
