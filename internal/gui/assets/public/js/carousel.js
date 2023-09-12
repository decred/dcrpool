//flickity carousel setup
var $carousel = $('.main-carousel').flickity({
    cellAlign: 'left',
    contain: true,
    wrapAround: true,
    arrowShape: {
        x0: 10,
        x1: 60, y1: 50,
        x2: 60, y2: 40,
        x3: 60
    },
    prevNextButtons: false,
    pageDots: false
});

var flkty = $carousel.data('flickity');
var $cellButtonGroup = $('.carousel-nav');
//add slide buttons
var total = flkty.slides.length;

for (i = 0; i < total; i++) {
    var title = $('.carousel-cell').eq(i).find('h2').text();
    if (i === 0) {
        $cellButtonGroup.append('<li class="carousel-nav__dot is-selected" title="' + title + '"></li>');
    } else if (i === total - 1) {
        $cellButtonGroup.append('<li class="carousel-nav__dot mr-0" title="' + title + '"></li>');
    } else {
        $cellButtonGroup.append('<li class="carousel-nav__dot" title="' + title + '"></li>');
    }
}

$('.carousel-nav').prepend('<li class="carousel-nav__previous"><img src="/assets/images/arrow-prev.svg"></li>');
$('.carousel-nav').append('<li class="carousel-nav__next"><img src="/assets/images/arrow-next.svg"></li>');

var $cellButtons = $cellButtonGroup.find('.carousel-nav__dot');

// update selected cellButtons
$carousel.on('select.flickity', function () {
    $cellButtons.filter('.is-selected')
        .removeClass('is-selected');
    $cellButtons.eq(flkty.selectedIndex)
        .addClass('is-selected');
});

// select cell on button click
$cellButtonGroup.on('click', '.carousel-nav__dot', function () {
    var index = $(this).index() - 1;
    $carousel.flickity('select', index);
});

// previous
$('.carousel-nav__previous').on('click', function () {
    $carousel.flickity('previous');
});
// next
$('.carousel-nav__next').on('click', function () {
    $carousel.flickity('next');
});

