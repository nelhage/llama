FROM amazonlinux:2
RUN yum -y install optipng zip
RUN mkdir /optipng
RUN cp -a /usr/bin/optipng /lib64/libpng* /optipng/
ADD optipng.sh /optipng/
RUN zip -j -r -o /optipng.zip /optipng/
