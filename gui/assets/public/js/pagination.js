$('#mined-blocks-page-select').pagination({
    dataSource: "/mined_blocks",
    autoHideNext: false,
    autoHidePrevious: false,
    hideWhenLessThanOnePage: true,
    pageSize: 10,
    nextText: '<div class="pagination-arrow pagination-arrow-right"></div>',
    prevText: '<div class="pagination-arrow pagination-arrow-left"></div>',
    locator: "blocks",
    totalNumberLocator: function(response) {
        return response.count;
    },
    callback: function(data) {
        var html = '';

        if (data.length > 0) {
            $.each(data, function(index, item){
                html += '<tr><td><a href="' + item.blockurl + '" rel="noopener noreferrer">' + item.blockheight + '</a></td><td>' + item.miner + '</td><td><span class="dcr-label">' + item.minedby + '</span></td></tr>';
            });
        } else {
            html += '<tr><td colspan="100%"><span class="no-data">No mined blocks</span></td></tr>';
        }
        $('#mined-blocks-table-body').html(html);
    }
})